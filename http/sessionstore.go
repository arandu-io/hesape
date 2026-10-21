package http

import (
	"context"
	stdhttp "net/http"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/session"
)

// SessionStore issues and validates the session of a signed-in subject, and
// remembers where a refused request was going.
//
// It is session.RecordStore of auth.Subject with the address beside it: the
// record store keeps the session, and an Intended over the same application
// key keeps the destination, which is an address and so not the session's to
// validate. Load answers the Subject inside the record, with the two fields
// the store owns -- Remembered and PasswordConfirmedAt -- folded back onto it.
//
// The cookie is session.CookieName, signed with the application key, so a
// browser holding a session issued through a RecordStore over the same key is
// signed in for this one too.
type SessionStore struct {
	store    *session.RecordStore[auth.Subject]
	intended *Intended
}

// NewSessionStore returns a store. Pass secure=false only in development:
// without the Secure attribute the cookie travels over plain HTTP.
//
// ttl is how long a session lives without Remember. A nil backend keeps the
// sessions in this process, which is right for one instance and wrong for
// two; see MemoryBackend.
func NewSessionStore(appKey []byte, ttl time.Duration, secure bool, b SessionBackend) *SessionStore {
	if b == nil {
		b = NewMemoryBackend()
	}
	return &SessionStore{
		store:    session.NewRecordStore[auth.Subject](appKey, ttl, secure, backendHandler{backend: b}),
		intended: NewIntended(appKey, secure),
	}
}

// Start creates a session for the subject and writes the cookie.
func (s *SessionStore) Start(ctx context.Context, w stdhttp.ResponseWriter, sub auth.Subject, opts ...session.Option) (string, error) {
	return s.store.Start(ctx, w, recordFor(sub), opts...)
}

// Rotate issues a new session id for the same subject and destroys the old
// one.
//
// It MUST be called on sign-in: keeping the pre-login id is session fixation.
// An empty oldID is exactly Start, so a sign-in on a request that carries no
// session is written with this name too.
func (s *SessionStore) Rotate(ctx context.Context, w stdhttp.ResponseWriter, oldID string, sub auth.Subject, opts ...session.Option) (string, error) {
	return s.store.Regenerate(ctx, w, oldID, recordFor(sub), opts...)
}

// Load returns the subject bound to the request's session cookie, or
// session.ErrNoSession or session.ErrExpired.
func (s *SessionStore) Load(ctx context.Context, r *stdhttp.Request) (auth.Subject, error) {
	rec, err := s.store.All(ctx, r)
	if err != nil {
		return auth.Subject{}, err
	}
	return subjectFrom(rec), nil
}

// Confirm records on the request's session that the subject has just typed
// their password again.
//
// It returns session.ErrConfirmationNotStored when the backend accepted the
// stamp and did not keep it. A handler must report that rather than redirect:
// redirecting sends the person back to the screen they just got right.
func (s *SessionStore) Confirm(ctx context.Context, w stdhttp.ResponseWriter, r *stdhttp.Request) error {
	return s.store.Confirm(ctx, w, r)
}

// DestroyOthers signs the subject out of every session except keepID.
//
// The tenant comes from the subject, which came from the Grant or the
// session, never from the request. A subject with no tenant is refused.
func (s *SessionStore) DestroyOthers(ctx context.Context, sub auth.Subject, keepID string) error {
	return s.store.DestroyOthers(ctx, recordFor(sub), keepID)
}

// Destroy removes the session and clears the cookies this store put in the
// browser -- the session, and the intended destination.
//
// The second is not tidiness. Signing out is the moment a shared machine
// changes hands, and the intended address outlives it by up to
// IntendedLifetime: whoever signs in next would be carried to the page the
// previous person was refused.
func (s *SessionStore) Destroy(ctx context.Context, w stdhttp.ResponseWriter, id string) error {
	if err := s.store.Invalidate(ctx, w, id); err != nil {
		return err
	}
	s.intended.Clear(w)
	return nil
}

// IDFromRequest returns the session id when the cookie signature is valid,
// and the empty string otherwise. It is the function to hand to the CSRF
// middleware, which binds its token to this id.
func (s *SessionStore) IDFromRequest(r *stdhttp.Request) string { return s.store.ID(r) }

// RememberIntended records where this request was going, so the sign-in
// screen it is about to be sent to can finish the journey.
func (s *SessionStore) RememberIntended(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	s.intended.Remember(w, r)
}

// TakeIntended returns the address RememberIntended stored, and clears it;
// fallback when there is none.
func (s *SessionStore) TakeIntended(w stdhttp.ResponseWriter, r *stdhttp.Request, fallback string) string {
	return s.intended.Take(w, r, fallback)
}

// PasswordConfirmedWithin reports whether the password was typed again on the
// subject's session less than window ago.
//
// It is a function because Subject belongs to hesape/auth and a method cannot
// be declared on another package's type. It asks the session record the same
// question session.Record.PasswordConfirmedWithin answers, so the refusals --
// no stamp, a stamp in the future, a window of zero -- are decided by one
// piece of code.
func PasswordConfirmedWithin(sub auth.Subject, window time.Duration) bool {
	return recordFor(sub).PasswordConfirmedWithin(window)
}

// SessionBackend stores the subject behind a session id, under the four
// method names a SessionStore backend has always been written against.
//
// session.Handler is the same contract under other names -- Get is Read, Put
// is Write, Delete is Destroy and DeleteSubject is DestroyIndex -- with the
// record in place of the subject. A handler written for that interface, such
// as a distributed one, is presented as a SessionBackend by NewSessionBackend.
type SessionBackend interface {
	// Get returns the subject, or session.ErrExpired when the backend does not
	// hold the id -- expired, evicted, or destroyed by a logout elsewhere.
	Get(ctx context.Context, id string) (auth.Subject, error)

	// Put stores the subject under id for the given ttl. It must store every
	// exported field of the Subject it is given: a field it silently drops
	// reads back as the zero value, and only in the deployment that uses that
	// backend.
	Put(ctx context.Context, id string, s auth.Subject, ttl time.Duration) error

	// Delete removes the session, if present.
	Delete(ctx context.Context, id string) error

	// DeleteSubject removes every session belonging to one subject of one
	// tenant, except keepID. An empty keepID keeps none of them. An empty
	// tenant or an empty subject id is an error.
	DeleteSubject(ctx context.Context, tenant, subjectID, keepID string) error
}

// recordFor is the record a session holds for sub: the index -- the tenant,
// the account and the two fields that decide how long it lives -- beside the
// subject as payload.
func recordFor(sub auth.Subject) session.Record[auth.Subject] {
	return session.Record[auth.Subject]{
		Payload:             sub,
		Tenant:              sub.Tenant,
		SubjectID:           sub.ID,
		Remembered:          sub.Remembered,
		PasswordConfirmedAt: sub.PasswordConfirmedAt,
	}
}

// subjectFrom is the subject a record holds, with the two fields the store
// owns folded back onto it, which is where a caller of Load reads them.
func subjectFrom(rec session.Record[auth.Subject]) auth.Subject {
	sub := rec.Payload
	sub.Remembered = rec.Remembered
	sub.PasswordConfirmedAt = rec.PasswordConfirmedAt
	return sub
}

// backendHandler presents a SessionBackend as a session.Handler.
type backendHandler struct{ backend SessionBackend }

var _ session.Handler[auth.Subject] = backendHandler{}

func (h backendHandler) Read(ctx context.Context, id string) (session.Record[auth.Subject], error) {
	sub, err := h.backend.Get(ctx, id)
	if err != nil {
		return session.Record[auth.Subject]{}, err
	}
	return recordFor(sub), nil
}

func (h backendHandler) Write(ctx context.Context, id string, rec session.Record[auth.Subject], ttl time.Duration) error {
	return h.backend.Put(ctx, id, subjectFrom(rec), ttl)
}

func (h backendHandler) Destroy(ctx context.Context, id string) error {
	return h.backend.Delete(ctx, id)
}

func (h backendHandler) DestroyIndex(ctx context.Context, tenant, subjectID, keepID string) error {
	return h.backend.DeleteSubject(ctx, tenant, subjectID, keepID)
}

// NewSessionBackend presents a session handler as a SessionBackend.
//
//	handler := <driver>.NewCacheBasedSessionHandler[auth.Subject](conn)
//	store := hhttp.NewSessionStore(key, ttl, true, hhttp.NewSessionBackend(handler))
//
// The four renames are all there is. The refusal crosses unchanged: a handler
// that does not hold the id answers session.ErrExpired, and that is what Get
// returns, so a caller branching on it behaves the same whichever store is
// wired.
//
// A nil handler is a wiring mistake rather than a request for the in-memory
// store, and it panics on the first session. Substituting memory would give a
// fleet of replicas a session store per process, which is the failure the
// distributed handler was wired to prevent.
func NewSessionBackend(h session.Handler[auth.Subject]) SessionBackend {
	return handlerBackend{handler: h}
}

// handlerBackend presents a session.Handler as a SessionBackend.
type handlerBackend struct{ handler session.Handler[auth.Subject] }

var _ SessionBackend = handlerBackend{}

func (b handlerBackend) Get(ctx context.Context, id string) (auth.Subject, error) {
	rec, err := b.handler.Read(ctx, id)
	if err != nil {
		return auth.Subject{}, err
	}
	return subjectFrom(rec), nil
}

func (b handlerBackend) Put(ctx context.Context, id string, s auth.Subject, ttl time.Duration) error {
	return b.handler.Write(ctx, id, recordFor(s), ttl)
}

func (b handlerBackend) Delete(ctx context.Context, id string) error {
	return b.handler.Destroy(ctx, id)
}

func (b handlerBackend) DeleteSubject(ctx context.Context, tenant, subjectID, keepID string) error {
	return b.handler.DestroyIndex(ctx, tenant, subjectID, keepID)
}

// MemoryBackend keeps sessions in the process memory.
//
// It is the right choice for development and for a single instance. Behind
// more than one instance it signs people out on every deploy and on every
// request routed elsewhere -- wire a distributed handler through
// NewSessionBackend.
type MemoryBackend struct {
	handler *session.ArrayHandler[auth.Subject]
}

var _ SessionBackend = (*MemoryBackend)(nil)

// NewMemoryBackend returns an empty in-memory session backend.
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{handler: session.NewArrayHandler[auth.Subject]()}
}

// Get returns the subject, or session.ErrExpired when the id is unknown.
func (m *MemoryBackend) Get(ctx context.Context, id string) (auth.Subject, error) {
	rec, err := m.handler.Read(ctx, id)
	if err != nil {
		return auth.Subject{}, err
	}
	return subjectFrom(rec), nil
}

// Put stores the subject under id for the given ttl.
func (m *MemoryBackend) Put(ctx context.Context, id string, s auth.Subject, ttl time.Duration) error {
	return m.handler.Write(ctx, id, recordFor(s), ttl)
}

// Delete removes the session, if present.
func (m *MemoryBackend) Delete(ctx context.Context, id string) error {
	return m.handler.Destroy(ctx, id)
}

// DeleteSubject removes every session of one subject of one tenant except
// keepID. An empty tenant or an empty subject id is refused.
func (m *MemoryBackend) DeleteSubject(ctx context.Context, tenant, subjectID, keepID string) error {
	return m.handler.DestroyIndex(ctx, tenant, subjectID, keepID)
}
