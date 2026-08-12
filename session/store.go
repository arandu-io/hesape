package session

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
)

// The keys the store reserves for itself. They are Illuminate's, character for
// character, because a session written by one and read by the other has to
// agree on where the token and the flash live.
const (
	// TokenKey holds the CSRF token. Illuminate's Store::token() reads it.
	TokenKey = "_token"
	// OldInputKey holds what was typed into the form that was rejected.
	OldInputKey = "_old_input"
	// PreviousURLKey holds the address to send somebody back to.
	PreviousURLKey = "_previous.url"
	// PasswordConfirmedKey holds when the password was last typed again, as a
	// unix timestamp.
	PasswordConfirmedKey = "auth.password_confirmed_at"

	// flashNewKey names the keys flashed by THIS request, which the next one
	// reads. flashOldKey names the ones flashed by the previous request, which
	// this one reads and which are dropped at the end of it.
	flashNewKey = "_flash.new"
	flashOldKey = "_flash.old"
)

// idLength is how many characters a session id has, and what [Store.IsValidID]
// insists on. Forty alphanumerics is Illuminate's Str::random(40): 238 bits,
// which is not a number anybody walks.
const idLength = 40

// ErrNoPreviousURL is returned by [Store.PreviousURI] when nothing has been
// stored. It is the RuntimeException Illuminate throws, as the error half of
// the pair.
var ErrNoPreviousURL = errors.New("session: no previous URL was stored")

// SessionHandler is where a session's bytes live between requests.
//
// It is PHP's SessionHandlerInterface, which Illuminate's handlers implement,
// with one mechanical change: every method takes a context.Context. PHP has no
// cancellation to plumb and Go does, and without it the database and cache
// handlers cannot honour a request that was abandoned or a deadline that has
// passed -- which on a session read is a goroutine held open for every client
// that walked away.
//
// Read answers with the empty string for a session that is not there. That is
// PHP's contract and it is kept, because it is what makes a first request and an
// expired one the same case: both start an empty session, and neither is an
// error the application can do anything about.
type SessionHandler interface {
	// Open is called before the first read. Every handler here answers nil; it
	// exists because the interface it mirrors has it, and because a handler that
	// opens a connection has somewhere to do it.
	Open(ctx context.Context, savePath, sessionName string) error
	// Close is called when the session is finished with.
	Close(ctx context.Context) error
	// Read returns the stored payload, or "" when there is none.
	Read(ctx context.Context, sessionID string) (string, error)
	// Write stores the payload under the id.
	Write(ctx context.Context, sessionID, data string) error
	// Destroy removes the session.
	Destroy(ctx context.Context, sessionID string) error
	// GC removes every session idle for longer than lifetime, and reports how
	// many went. A handler whose store expires entries by itself answers 0.
	GC(ctx context.Context, lifetime time.Duration) (int, error)
}

// ExistenceAwareInterface is implemented by a handler that has to be told
// whether the session it is about to write already exists.
//
// Only [DatabaseSessionHandler] needs it, and it needs it for one reason: a row
// is an INSERT or an UPDATE, and getting that wrong is either a duplicate key
// or an update that touches nothing. [Store.Migrate] tells it, because migrating
// is the moment the id stops being one the table has.
type ExistenceAwareInterface interface {
	SetExists(value bool)
}

// RequestAware is implemented by a handler that stores the session in the
// request itself.
//
// Only [CookieSessionHandler] does. Illuminate asks `instanceof
// CookieSessionHandler` in Store::handlerNeedsRequest(); this asks for the
// capability instead, which answers the same for that handler and does not have
// to be edited when a second one needs the same thing.
type RequestAware interface {
	SetRequest(r *http.Request)
}

// Store is one session: Illuminate\Session\Store.
//
// It is a bag of keys with an id, loaded from a [SessionHandler] at the start of
// a request and written back at the end of it. What lives in it is the flash,
// the old input, the CSRF token and the previous URL -- which is to say
// everything that makes old() and $errors work on the page a rejected form is
// sent back to.
//
// # One Store belongs to one request
//
// It holds no lock, exactly as Illuminate's does not. It is created for a
// request by [SessionManager.Driver] or by [StartSession], read and written on
// that request's goroutine, and saved at the end of it. Sharing one between
// requests is the bug this paragraph exists to name: two requests would
// interleave writes into one map and each would save the other's session.
//
// # This is not [RecordStore]
//
// [RecordStore] is the other half -- it signs the cookie, mints ids and stores
// one typed [Record] per session. This one is the bag of keys a Laravel
// developer reaches for. They share nothing but the package.
type Store struct {
	id         string
	name       string
	attributes map[string]any
	handler    SessionHandler
	started    bool

	// prepareForUnserialize and prepareForStorage are the two hooks
	// [EncryptedStore] replaces. They are fields and not methods because Go has
	// no virtual dispatch: an EncryptedStore that embedded a Store and shadowed
	// two methods would find Save still calling the Store's own, and the session
	// would be written in the clear with nothing to show for it. A field is
	// replaced once, in the constructor, and every caller goes through it.
	prepareForUnserialize func(data string) (string, error)
	prepareForStorage     func(data string) (string, error)
}

// NewStore returns a session over a handler.
//
// name is the cookie the id travels in, which is Illuminate's session.cookie.
// id is the one the browser sent; an empty or malformed one is replaced with a
// fresh one, which is what Illuminate's setId() does and is why a forged cookie
// starts an empty session rather than an error.
//
// Illuminate's constructor takes a fourth argument choosing between PHP's
// serialize() and JSON. There is no serialize() in Go, so the payload is always
// JSON -- which is also the only one of the two that a second language can read,
// and the reason Illuminate added the option.
func NewStore(name string, handler SessionHandler, id string) *Store {
	s := &Store{
		name:                  name,
		attributes:            map[string]any{},
		handler:               handler,
		prepareForUnserialize: func(data string) (string, error) { return data, nil },
		prepareForStorage:     func(data string) (string, error) { return data, nil },
	}
	s.SetID(id)
	return s
}

// Start loads the session from the handler and mints a CSRF token when there is
// none.
//
// It is the first thing [StartSession] does, and it must run before anything
// reads the session: an unstarted Store is an empty one, and a page drawn from
// it shows no errors and no old input.
func (s *Store) Start(ctx context.Context) error {
	if err := s.loadSession(ctx); err != nil {
		return err
	}
	if !s.Has(TokenKey) {
		s.RegenerateToken()
	}
	s.started = true
	return nil
}

// loadSession merges what the handler holds over what is already in the bag.
func (s *Store) loadSession(ctx context.Context) error {
	stored, err := s.readFromHandler(ctx)
	if err != nil {
		return err
	}
	for key, value := range stored {
		s.attributes[key] = value
	}
	return nil
}

// readFromHandler reads and decodes, and treats an unreadable payload as an
// empty session.
//
// Illuminate does the same with a `@unserialize` whose failure it swallows, and
// the reason is worth keeping: a payload this binary cannot read is one written
// by an older release or corrupted in transit, and the person in front of it can
// do nothing about either. Signing them out is survivable; a 500 on every page
// until somebody clears their cookies is not.
func (s *Store) readFromHandler(ctx context.Context) (map[string]any, error) {
	data, err := s.handler.Read(ctx, s.GetID())
	if err != nil {
		return nil, err
	}
	if data == "" {
		return nil, nil
	}
	plain, err := s.prepareForUnserialize(data)
	if err != nil {
		// An [EncryptedStore] that cannot decrypt what is stored is holding a
		// session written under another key -- a rotation, or somebody else's
		// cookie. Illuminate answers the same way, with an empty array: the
		// person is signed out, which is survivable, and an error here would be
		// a 500 on every page until they cleared their cookies.
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(plain), &out); err != nil {
		return nil, nil
	}
	return out, nil
}

// Save writes the session back to the handler and ages the flash.
//
// Ageing on the way out is what makes a flashed message survive exactly one
// request: what this request flashed becomes what the next one reads, and what
// this request read is dropped. See [Store.AgeFlashData].
func (s *Store) Save(ctx context.Context) error {
	s.AgeFlashData()

	encoded, err := json.Marshal(s.attributes)
	if err != nil {
		return fmt.Errorf("session: encoding the session: %w", err)
	}
	payload, err := s.prepareForStorage(string(encoded))
	if err != nil {
		return err
	}
	if err := s.handler.Write(ctx, s.GetID(), payload); err != nil {
		return err
	}
	s.started = false
	return nil
}

// AgeFlashData moves this request's flash into the next request's, and drops
// the previous one.
//
// This is the whole of how a message survives a redirect and no longer:
//
//  1. the keys flashed by the PREVIOUS request -- which this one has just
//     finished reading -- are forgotten;
//  2. the keys flashed by THIS request become the ones the next request reads;
//  3. this request's list is emptied.
//
// Getting the order wrong is the bug worth naming, because it does not look like
// one: forget after the move and the message is gone before anybody sees it;
// forget never and it appears again on a page nobody submitted, where the person
// reading it has no way to tell it is stale.
func (s *Store) AgeFlashData() {
	s.Forget(s.flashKeys(flashOldKey)...)
	s.Put(flashOldKey, s.flashKeys(flashNewKey))
	s.Put(flashNewKey, []string{})
}

// All returns everything the session holds.
func (s *Store) All() map[string]any {
	out := make(map[string]any, len(s.attributes))
	for key, value := range s.attributes {
		out[key] = value
	}
	return out
}

// Only returns the named keys.
func (s *Store) Only(keys []string) map[string]any {
	out := map[string]any{}
	for _, key := range keys {
		if value, ok := arrGet(s.attributes, key); ok {
			out[key] = value
		}
	}
	return out
}

// Except returns everything but the named keys.
func (s *Store) Except(keys []string) map[string]any {
	out := s.All()
	for _, key := range keys {
		arrForget(out, key)
	}
	return out
}

// Exists reports whether every key is present, whatever it holds -- including
// nil. [Store.Has] is the one that asks for a value as well.
//
// No keys is false, and Illuminate's is true: `!collect([])->contains(...)` is
// what PHP makes of the empty case, and it is an artifact rather than a
// decision. False is the answer that does not make `if !s.Exists(keys...)` pass
// silently for a list something upstream forgot to fill.
func (s *Store) Exists(keys ...string) bool {
	for _, key := range keys {
		if _, ok := arrGet(s.attributes, key); !ok {
			return false
		}
	}
	return len(keys) > 0
}

// Missing reports whether none of the keys is present.
func (s *Store) Missing(keys ...string) bool { return !s.Exists(keys...) }

// Has reports whether every key is present and holds something. No keys is
// false, for the reason [Store.Exists] gives.
func (s *Store) Has(keys ...string) bool {
	for _, key := range keys {
		value, ok := arrGet(s.attributes, key)
		if !ok || value == nil {
			return false
		}
	}
	return len(keys) > 0
}

// HasAny reports whether at least one of the keys is present and holds
// something.
func (s *Store) HasAny(keys ...string) bool {
	for _, key := range keys {
		if value, ok := arrGet(s.attributes, key); ok && value != nil {
			return true
		}
	}
	return false
}

// Get returns a value, or the default when there is none.
//
// The key is read in dot notation, so "user.name" reaches inside a nested map,
// which is what Illuminate's Arr::get does. The default is variadic because
// PHP's is an optional argument; passing none means nil.
//
// It answers `any`, because the session holds `mixed`. What comes back out of a
// session that has been through a handler is what JSON decodes to: a number is
// a float64, an array is a []any. Reach for [Store.Increment] rather than
// asserting on the number type yourself.
func (s *Store) Get(key string, def ...any) any {
	if value, ok := arrGet(s.attributes, key); ok {
		return value
	}
	if len(def) > 0 {
		return def[0]
	}
	return nil
}

// Pull returns a value and forgets it.
func (s *Store) Pull(key string, def ...any) any {
	value := s.Get(key, def...)
	arrForget(s.attributes, key)
	return value
}

// HasOldInput reports whether the rejected form left anything behind. An empty
// key asks whether there is any old input at all.
func (s *Store) HasOldInput(key string) bool {
	old := s.GetOldInput(key)
	if key == "" {
		values, ok := old.(map[string]any)
		return ok && len(values) > 0
	}
	return old != nil
}

// GetOldInput returns what was typed into the form that was rejected, so the
// page it was sent back to can put it in the boxes again. An empty key returns
// all of it.
func (s *Store) GetOldInput(key string, def ...any) any {
	old, _ := s.Get(OldInputKey, map[string]any{}).(map[string]any)
	if old == nil {
		old = map[string]any{}
	}
	if key == "" {
		return old
	}
	if value, ok := arrGet(old, key); ok {
		return value
	}
	if len(def) > 0 {
		return def[0]
	}
	return nil
}

// Replace puts every pair of the map in the session, leaving the rest alone.
//
// It is Illuminate's replace(), which is put() given an array -- the name says
// "replace" and the body replaces the named keys, not the session. [Store.Flush]
// is the one that empties it.
func (s *Store) Replace(attributes map[string]any) {
	for key, value := range attributes {
		s.Put(key, value)
	}
}

// Put writes a value. The key is dot notation, so "user.name" writes inside a
// nested map, creating it when it is not there.
func (s *Store) Put(key string, value any) { arrSet(s.attributes, key, value) }

// Remember returns a value, storing what the callback produces when there is
// none.
func (s *Store) Remember(key string, callback func() any) any {
	if value := s.Get(key); value != nil {
		return value
	}
	value := callback()
	s.Put(key, value)
	return value
}

// Push appends a value to a list in the session, starting one when there is
// none.
func (s *Store) Push(key string, value any) {
	list, _ := toSlice(s.Get(key))
	s.Put(key, append(list, value))
}

// Increment adds to a number in the session and returns what it became.
//
// A key holding nothing counts as zero, which is what makes it usable as a
// counter without a branch at the call site. A key holding something that is not
// a number is treated as zero too: the alternative is an error on a path whose
// whole purpose is not to have one, and a counter that silently restarts is
// visible in a way a swallowed error is not.
func (s *Store) Increment(key string, amount int) int {
	value := toInt(s.Get(key, 0)) + amount
	s.Put(key, value)
	return value
}

// Decrement subtracts from a number in the session and returns what it became.
func (s *Store) Decrement(key string, amount int) int { return s.Increment(key, -amount) }

// Flash stores a value for the NEXT request and no longer.
//
// It is what a redirect after a rejected form carries: the controller flashes,
// answers a redirect, and the page the browser lands on reads it. By the request
// after that it is gone -- [Store.Save] ages it, which is what stops a message
// resurfacing on a page nobody submitted.
//
// The key is removed from the previous request's list at the same time, so
// flashing something that was already flashed extends it rather than leaving two
// entries that expire at different moments.
func (s *Store) Flash(key string, value any) {
	s.Put(key, value)
	s.Push(flashNewKey, key)
	s.removeFromOldFlashData(key)
}

// Now stores a value for THIS request only.
//
// It goes straight into the previous request's list, so [Store.AgeFlashData]
// drops it at the end of this one. It is for a message produced by the request
// that is also going to draw the page -- no redirect in between.
func (s *Store) Now(key string, value any) {
	s.Put(key, value)
	s.Push(flashOldKey, key)
}

// Reflash keeps everything flashed for this request for one more.
//
// It is what a request that could not draw the page calls -- a redirect in the
// middle of a flow, an HTMX fragment that turned out to need a full navigation
// -- so the messages survive to the page that can show them.
func (s *Store) Reflash() {
	s.mergeNewFlashes(s.flashKeys(flashOldKey)...)
	s.Put(flashOldKey, []string{})
}

// Keep reflashes only the named keys.
func (s *Store) Keep(keys ...string) {
	s.mergeNewFlashes(keys...)
	s.removeFromOldFlashData(keys...)
}

// mergeNewFlashes adds keys to the next request's list, without duplicates and
// in a stable order -- a list whose order changes between two identical requests
// is one nobody can reproduce a report of.
func (s *Store) mergeNewFlashes(keys ...string) {
	merged := s.flashKeys(flashNewKey)
	for _, key := range keys {
		if !slices.Contains(merged, key) {
			merged = append(merged, key)
		}
	}
	sort.Strings(merged)
	s.Put(flashNewKey, merged)
}

// removeFromOldFlashData drops keys from the previous request's list, so
// ageing does not forget a value that was just re-flashed.
func (s *Store) removeFromOldFlashData(keys ...string) {
	kept := []string{}
	for _, key := range s.flashKeys(flashOldKey) {
		if !slices.Contains(keys, key) {
			kept = append(kept, key)
		}
	}
	s.Put(flashOldKey, kept)
}

// flashKeys reads one of the two lists, tolerating the []any a JSON round trip
// turns a []string into.
func (s *Store) flashKeys(which string) []string {
	return toStrings(s.Get(which, []string{}))
}

// FlashInput stores what was typed into a form, so the page it is sent back to
// can fill the boxes in again. It is read with [Store.GetOldInput].
func (s *Store) FlashInput(value map[string]any) { s.Flash(OldInputKey, value) }

// Remove forgets a key and returns what it held.
func (s *Store) Remove(key string) any {
	value := s.Get(key)
	arrForget(s.attributes, key)
	return value
}

// Forget removes keys from the session.
func (s *Store) Forget(keys ...string) {
	for _, key := range keys {
		arrForget(s.attributes, key)
	}
}

// Flush empties the session, keeping the id.
func (s *Store) Flush() { s.attributes = map[string]any{} }

// Invalidate empties the session AND gives it a new id, destroying the old one.
//
// It is what a sign-out calls. Both halves matter: flushing without a new id
// leaves the browser holding an id somebody may already know, and a new id
// without flushing leaves the old row readable to anything that has it.
func (s *Store) Invalidate(ctx context.Context) error {
	s.Flush()
	return s.Migrate(ctx, true)
}

// Regenerate gives the session a new id and a new CSRF token.
//
// It MUST be called on sign-in. Keeping the id somebody arrived with is session
// fixation: an attacker plants an id, waits for the victim to authenticate on
// it, and is then signed in as them. destroy removes the old record as well,
// which is what you want unless something else is still reading it.
func (s *Store) Regenerate(ctx context.Context, destroy bool) error {
	if err := s.Migrate(ctx, destroy); err != nil {
		return err
	}
	s.RegenerateToken()
	return nil
}

// Migrate gives the session a new id, optionally destroying the old record.
//
// It is [Store.Regenerate] without the new token, which is what
// [Store.Invalidate] wants: there is nobody left to hand a token to.
func (s *Store) Migrate(ctx context.Context, destroy bool) error {
	if destroy {
		if err := s.handler.Destroy(ctx, s.GetID()); err != nil {
			return err
		}
	}
	s.SetExists(false)
	s.SetID("")
	return nil
}

// IsStarted reports whether [Store.Start] has run and [Store.Save] has not.
func (s *Store) IsStarted() bool { return s.started }

// GetName returns the cookie the session id travels in.
func (s *Store) GetName() string { return s.name }

// SetName sets it.
func (s *Store) SetName(name string) { s.name = name }

// ID returns the session id. It is [Store.GetID] under Illuminate's other name
// for it, and both are here because Illuminate has both.
func (s *Store) ID() string { return s.GetID() }

// GetID returns the session id.
func (s *Store) GetID() string { return s.id }

// SetID sets the session id, or mints one when what it is given is not a
// session id.
//
// An empty or malformed id is replaced rather than refused, which is what makes
// a first visit and a forged cookie the same case: both get a fresh, empty
// session. Refusing would turn a cookie somebody can set at will into an error
// page they can force on anybody.
func (s *Store) SetID(id string) {
	if s.IsValidID(id) {
		s.id = id
		return
	}
	s.id = generateSessionID()
}

// IsValidID reports whether a string is shaped like a session id: exactly
// [idLength] characters, letters and digits only.
//
// The shape check is what keeps a cookie value out of the handler. A file
// handler joins the id onto a path and a database handler puts it in a WHERE:
// neither should ever see "../../etc/passwd", and this is the line that means
// neither can.
func (s *Store) IsValidID(id string) bool {
	if len(id) != idLength {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// generateSessionID draws [idLength] alphanumerics from crypto/rand.
func generateSessionID() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, idLength)
	// crypto/rand.Read is documented never to fail, and panics internally if the
	// operating system's source ever does. There is no error to handle.
	rand.Read(b)
	for i, v := range b {
		b[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(b)
}

// SetExists tells a handler that has to know whether the row is already there.
// A handler that does not implement [ExistenceAwareInterface] is not told, and
// does not need to be.
func (s *Store) SetExists(value bool) {
	if aware, ok := s.handler.(ExistenceAwareInterface); ok {
		aware.SetExists(value)
	}
}

// Token returns the CSRF token this session carries.
func (s *Store) Token() string {
	token, _ := s.Get(TokenKey, "").(string)
	return token
}

// RegenerateToken mints a new CSRF token.
func (s *Store) RegenerateToken() { s.Put(TokenKey, generateSessionID()) }

// HasPreviousURI reports whether an address was stored to send somebody back
// to.
func (s *Store) HasPreviousURI() bool { return s.PreviousURL() != "" }

// PreviousURI returns the stored address, parsed.
//
// It is the (T, error) that stands in for the RuntimeException Illuminate
// throws when there is none: a caller that redirects to the zero URL sends
// somebody to the site root without meaning to.
func (s *Store) PreviousURI() (*url.URL, error) {
	previous := s.PreviousURL()
	if previous == "" {
		return nil, ErrNoPreviousURL
	}
	parsed, err := url.Parse(previous)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not one: %w", ErrNoPreviousURL, previous, err)
	}
	return parsed, nil
}

// PreviousURL returns the stored address, or "" when there is none.
func (s *Store) PreviousURL() string {
	previous, _ := s.Get(PreviousURLKey, "").(string)
	return previous
}

// SetPreviousURL stores where somebody was, so a guard can send them back after
// they sign in.
//
// It stores the address as it is given. Validating one is the caller's job and
// deliberately not this package's: an address read off a request and redirected
// to without a check is an open redirect, and the place to refuse it is where
// the request is understood -- see hesape/hhttp.
func (s *Store) SetPreviousURL(u string) { s.Put(PreviousURLKey, u) }

// PasswordConfirmed records that the subject has just typed their password
// again, as a unix timestamp under Illuminate's key.
func (s *Store) PasswordConfirmed() { s.Put(PasswordConfirmedKey, time.Now().Unix()) }

// GetHandler returns the handler underneath.
func (s *Store) GetHandler() SessionHandler { return s.handler }

// SetHandler replaces it and returns the one now in place.
func (s *Store) SetHandler(handler SessionHandler) SessionHandler {
	s.handler = handler
	return s.handler
}

// HandlerNeedsRequest reports whether the handler stores the session in the
// request itself, which only [CookieSessionHandler] does.
func (s *Store) HandlerNeedsRequest() bool {
	_, ok := s.handler.(RequestAware)
	return ok
}

// SetRequestOnHandler hands the request to a handler that needs one, and does
// nothing for one that does not.
func (s *Store) SetRequestOnHandler(r *http.Request) {
	if aware, ok := s.handler.(RequestAware); ok {
		aware.SetRequest(r)
	}
}

// arrGet reads a dot-notation key. The second result separates "present and
// nil" from "not there", which is the whole difference between Exists and Has.
func arrGet(attributes map[string]any, key string) (any, bool) {
	if key == "" {
		return nil, false
	}
	if value, ok := attributes[key]; ok {
		return value, true
	}
	current := any(attributes)
	for _, segment := range strings.Split(key, ".") {
		level, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = level[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// arrSet writes a dot-notation key, creating the levels it needs.
//
// A level holding something that is not a map is replaced. Illuminate's Arr::set
// does the same, and the alternative -- refusing -- would make a session that
// once held a string at "user" permanently unable to hold "user.name".
func arrSet(attributes map[string]any, key string, value any) {
	if key == "" {
		return
	}
	segments := strings.Split(key, ".")
	level := attributes
	for _, segment := range segments[:len(segments)-1] {
		next, ok := level[segment].(map[string]any)
		if !ok {
			next = map[string]any{}
			level[segment] = next
		}
		level = next
	}
	level[segments[len(segments)-1]] = value
}

// arrForget removes a dot-notation key.
func arrForget(attributes map[string]any, key string) {
	if key == "" {
		return
	}
	if _, ok := attributes[key]; ok {
		delete(attributes, key)
		return
	}
	segments := strings.Split(key, ".")
	level := attributes
	for _, segment := range segments[:len(segments)-1] {
		next, ok := level[segment].(map[string]any)
		if !ok {
			return
		}
		level = next
	}
	delete(level, segments[len(segments)-1])
}

// toSlice reads a list back, tolerating the []any that a JSON round trip turns
// every list into.
func toSlice(value any) ([]any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case []any:
		return typed, true
	case []string:
		out := make([]any, len(typed))
		for i, v := range typed {
			out[i] = v
		}
		return out, true
	default:
		return nil, false
	}
}

// toStrings reads a list of strings back, dropping anything in it that is not
// one.
func toStrings(value any) []string {
	list, ok := toSlice(value)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// toInt reads a number back, tolerating the float64 a JSON round trip turns
// every number into. Anything that is not a number counts as zero.
func toInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, err := typed.Int64()
		if err != nil {
			return 0
		}
		return int(n)
	default:
		return 0
	}
}
