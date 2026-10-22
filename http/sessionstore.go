package http

import (
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"sync/atomic"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/session"
)

// SubjectResolver answers who the account a session was opened for is now: its
// roles, its actions, its tenant and whether its address is verified, read
// from where the application keeps them.
//
// who carries the identity the session holds and nothing else -- ID and
// Tenant -- so a resolver that hands back what it was given hands back a
// subject that can do nothing, never the copy taken at sign-in. A session is
// for one account in one tenant, and a session whose account cannot be found
// is over: answer ErrSubjectGone, or a subject whose ID or Tenant differs from
// who's, and the session is destroyed. Any other error fails the request that
// asked, and the stored copy is not used in its place.
//
// It runs on every Load, which is every request a guard reads a session for:
// one read of the account per authenticated request. A resolver may answer
// from a cache; the cache is then what decides how long a demoted role keeps
// working.
//
// The fields the session itself owns -- Remembered, PasswordConfirmedAt,
// Impersonator and Acting -- are the session's, and whatever the resolver puts
// in them is replaced.
type SubjectResolver func(ctx context.Context, who auth.Subject) (auth.Subject, error)

// Errors returned by SessionStore.
var (
	// ErrSubjectGone is what a SubjectResolver answers when the account a
	// session was opened for no longer exists, or no longer belongs to the
	// session's tenant. Load destroys that session and answers
	// session.ErrExpired, so a guard sends the person to the sign-in screen.
	ErrSubjectGone = errors.New("http: the account this session was opened for is gone")

	// ErrSubjectUnresolved wraps the error a SubjectResolver failed with. The
	// request fails rather than falling back on the subject the session was
	// opened with, because that copy is exactly what may no longer be true.
	ErrSubjectUnresolved = errors.New("http: the session's account could not be read")

	// ErrNoSubjectResolver is a subject with roles or actions met by a store
	// no SubjectResolver was given. Roles and actions written at sign-in and
	// read back for the life of the session would outlive a demotion by as
	// long as the session lasts, so Start and Rotate refuse to write them and
	// Load refuses to answer a session that holds them.
	ErrNoSubjectResolver = errors.New("http: a subject with roles or actions needs a SubjectResolver on the session store")
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
// The session keeps who the person is, not what they may do. With a
// SubjectResolver installed by ResolveSubjects, Load reads the account's
// roles, actions, tenant and verification afresh on every request, so a
// demotion or a removal takes effect on the next one. Without one, only
// subjects with neither roles nor actions are accepted.
//
// The cookie is session.CookieName, signed with the application key, so a
// browser holding a session issued through a RecordStore over the same key is
// signed in for this one too.
type SessionStore struct {
	store    *session.RecordStore[auth.Subject]
	intended *Intended
	resolver atomic.Pointer[SubjectResolver]
}

// ResolveSubjects installs the function Load reads the account's current
// subject through. It is called once, when the application is assembled, by
// the code that owns the accounts:
//
//	sessions.ResolveSubjects(func(ctx context.Context, who auth.Subject) (auth.Subject, error) {
//		user, err := users.FindForAuthentication(ctx, who.Tenant, who.ID)
//		if errors.Is(err, account.ErrUserNotFound) {
//			return auth.Subject{}, hhttp.ErrSubjectGone
//		}
//		if err != nil {
//			return auth.Subject{}, err
//		}
//		if users.Admit(ctx, user) != nil {
//			return auth.Subject{}, hhttp.ErrSubjectGone
//		}
//		return user.(*models.User).Subject(), nil
//	})
//
// It is a method rather than an argument of NewSessionStore because the store
// is built with the drivers, before the accounts it reads exist.
//
// A nil resolver, or a second call, panics: both are wiring mistakes, and a
// store whose resolver one part of the application replaced behind another's
// back is a store nobody can reason about.
func (s *SessionStore) ResolveSubjects(r SubjectResolver) {
	if r == nil {
		panic("http: ResolveSubjects was given a nil resolver")
	}
	if !s.resolver.CompareAndSwap(nil, &r) {
		panic("http: ResolveSubjects was called twice on one session store")
	}
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
//
// With a SubjectResolver installed, the session keeps the subject's identity
// and not its roles or actions, which Load reads afresh. Without one, a
// subject carrying roles or actions is refused with ErrNoSubjectResolver and
// no session is written.
func (s *SessionStore) Start(ctx context.Context, w stdhttp.ResponseWriter, sub auth.Subject, opts ...session.Option) (string, error) {
	kept, err := s.kept(sub)
	if err != nil {
		return "", err
	}
	return s.store.Start(ctx, w, recordFor(kept), opts...)
}

// Rotate issues a new session id for the same subject and destroys the old
// one.
//
// It MUST be called on sign-in: keeping the pre-login id is session fixation.
// An empty oldID is exactly Start, so a sign-in on a request that carries no
// session is written with this name too. It refuses what Start refuses, and
// leaves the old session as it was when it does.
func (s *SessionStore) Rotate(ctx context.Context, w stdhttp.ResponseWriter, oldID string, sub auth.Subject, opts ...session.Option) (string, error) {
	kept, err := s.kept(sub)
	if err != nil {
		return "", err
	}
	return s.store.Regenerate(ctx, w, oldID, recordFor(kept), opts...)
}

// Load returns the subject bound to the request's session cookie, or
// session.ErrNoSession or session.ErrExpired.
//
// With a SubjectResolver installed, the subject is the one it answers now for
// the session's account -- and for the operator viewing it, when one is --
// with the fields the session owns laid back on. An account the resolver says
// is gone, or answers for under another id or tenant, ends the session: it is
// destroyed and Load answers session.ErrExpired. A resolver that fails makes
// Load fail with ErrSubjectUnresolved, and the copy the session was opened
// with is never answered instead.
//
// Without one, a session holding roles or actions answers
// ErrNoSubjectResolver.
func (s *SessionStore) Load(ctx context.Context, r *stdhttp.Request) (auth.Subject, error) {
	rec, err := s.store.All(ctx, r)
	if err != nil {
		return auth.Subject{}, err
	}
	stored := subjectFrom(rec)
	resolve := s.resolver.Load()
	if resolve == nil {
		if decides(stored) {
			return auth.Subject{}, ErrNoSubjectResolver
		}
		return stored, nil
	}

	current, err := resolveOne(ctx, *resolve, stored)
	if err == nil && stored.Impersonator != nil {
		var operator auth.Subject
		if operator, err = resolveOne(ctx, *resolve, *stored.Impersonator); err == nil {
			// The operator's own session fields are the ones Impersonate kept
			// when the viewing began.
			operator.Remembered = stored.Impersonator.Remembered
			operator.PasswordConfirmedAt = stored.Impersonator.PasswordConfirmedAt
			current.Impersonator = &operator
			current.Acting = stored.Acting
		}
	}
	if errors.Is(err, ErrSubjectGone) {
		// The account is gone, or is no longer the tenant's: the session it
		// was opened for is over everywhere it is read, not only on this
		// request.
		if destroyErr := s.store.Invalidate(ctx, discard{}, s.store.ID(r)); destroyErr != nil {
			return auth.Subject{}, fmt.Errorf("%w: %w", ErrSubjectUnresolved, destroyErr)
		}
		return auth.Subject{}, fmt.Errorf("%w: %w", session.ErrExpired, err)
	}
	if err != nil {
		return auth.Subject{}, err
	}
	current.Remembered = stored.Remembered
	current.PasswordConfirmedAt = stored.PasswordConfirmedAt
	return current, nil
}

// resolveOne asks the resolver about the account stored names, giving it the
// identity and nothing else, and refuses an answer that is not that account
// in that tenant.
func resolveOne(ctx context.Context, resolve SubjectResolver, stored auth.Subject) (auth.Subject, error) {
	current, err := resolve(ctx, auth.Subject{ID: stored.ID, Tenant: stored.Tenant})
	switch {
	case errors.Is(err, ErrSubjectGone):
		return auth.Subject{}, err
	case err != nil:
		return auth.Subject{}, fmt.Errorf("%w: %w", ErrSubjectUnresolved, err)
	case current.ID == "" || current.ID != stored.ID || current.IsGuest():
		return auth.Subject{}, fmt.Errorf("%w: the resolver answered for another account", ErrSubjectGone)
	case current.Tenant == "" || current.Tenant != stored.Tenant:
		return auth.Subject{}, fmt.Errorf("%w: the account is no longer the session's tenant's", ErrSubjectGone)
	}
	// Who is viewing whom is the session's to say, never the resolver's.
	current.Impersonator, current.Acting = nil, false
	current.Remembered, current.PasswordConfirmedAt = false, time.Time{}
	return current, nil
}

// kept is the subject a session is written with: its identity alone when a
// resolver will supply the rest, and the subject as it is when no resolver is
// installed and it carries nothing a resolver would have to keep current.
func (s *SessionStore) kept(sub auth.Subject) (auth.Subject, error) {
	if s.resolver.Load() == nil {
		if decides(sub) {
			return auth.Subject{}, ErrNoSubjectResolver
		}
		return sub, nil
	}
	kept := sub
	kept.Roles, kept.Actions = nil, nil
	if sub.Impersonator != nil {
		operator := *sub.Impersonator
		operator.Roles, operator.Actions = nil, nil
		kept.Impersonator = &operator
	}
	return kept, nil
}

// decides reports whether a subject carries anything an authorization decision
// reads off it that a session must not freeze: roles or actions, its own or
// those of the operator viewing it.
func decides(sub auth.Subject) bool {
	if len(sub.Roles) > 0 || len(sub.Actions) > 0 {
		return true
	}
	return sub.Impersonator != nil && decides(*sub.Impersonator)
}

// discard is the response a session destroyed while it is read is cleared on.
// The cookie the browser holds is left: its record is gone, so it is a session
// that does not exist, and Load is not given a response to write to.
type discard struct{}

func (discard) Header() stdhttp.Header      { return stdhttp.Header{} }
func (discard) Write(b []byte) (int, error) { return len(b), nil }
func (discard) WriteHeader(int)             {}

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
