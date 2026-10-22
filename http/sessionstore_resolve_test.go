package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/session"
)

// directory is the account table the stores in this file resolve from.
type directory struct {
	mu    sync.Mutex
	rows  map[string]auth.Subject
	err   error
	asked []auth.Subject
}

func newDirectory(subs ...auth.Subject) *directory {
	d := &directory{rows: map[string]auth.Subject{}}
	for _, sub := range subs {
		d.put(sub)
	}
	return d
}

func (d *directory) put(sub auth.Subject) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rows[sub.Tenant+"/"+sub.ID] = sub
}

func (d *directory) resolve(_ context.Context, who auth.Subject) (auth.Subject, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asked = append(d.asked, who)
	if d.err != nil {
		return auth.Subject{}, d.err
	}
	row, ok := d.rows[who.Tenant+"/"+who.ID]
	if !ok {
		return auth.Subject{}, hhttp.ErrSubjectGone
	}
	return row, nil
}

// resolvedStore is a store over memory that resolves from d.
func resolvedStore(d *directory) *hhttp.SessionStore {
	s := hhttp.NewSessionStore(sessionAppKey, time.Hour, false, nil)
	s.ResolveSubjects(d.resolve)
	return s
}

// open starts a session for sub and returns a request carrying its cookie.
func open(t *testing.T, s *hhttp.SessionStore, sub auth.Subject) *http.Request {
	t.Helper()
	rec := httptest.NewRecorder()
	if _, err := s.Start(context.Background(), rec, sub); err != nil {
		t.Fatalf("Start: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	return r
}

func TestLoadAnswersTheAccountAsItIsNowAndNotAsItWasAtSignIn(t *testing.T) {
	d := newDirectory(auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"admin"}, Actions: []auth.Action{"invoice.delete"}})
	s := resolvedStore(d)
	r := open(t, s, auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"admin"}, Actions: []auth.Action{"invoice.delete"}})

	d.put(auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"reader"}, Verified: true})
	got, err := s.Load(context.Background(), r)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.HasRole("admin") || !got.HasRole("reader") || got.Can("invoice.delete") {
		t.Errorf("Load answered roles %v and actions %v, want the account's current ones", got.Roles, got.Actions)
	}
	if !got.Verified {
		t.Error("Load answered the address as unverified after the account verified it")
	}
}

// The resolver is told who, and only who: a resolver that answered with what
// it was given would otherwise hand back the roles the session was opened
// with.
func TestTheResolverIsGivenTheIdentityAndNothingItCouldEchoBack(t *testing.T) {
	d := newDirectory(auth.Subject{ID: "u-1", Tenant: "acme"})
	s := resolvedStore(d)
	r := open(t, s, auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"admin"}, Actions: []auth.Action{"x.y"}, Verified: true})
	if _, err := s.Load(context.Background(), r); err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, who := range d.asked {
		if who.ID != "u-1" || who.Tenant != "acme" || len(who.Roles) != 0 || len(who.Actions) != 0 || who.Verified {
			t.Errorf("the resolver was given %+v, want only the id and the tenant", who)
		}
	}

	echo := hhttp.NewSessionStore(sessionAppKey, time.Hour, false, nil)
	echo.ResolveSubjects(func(_ context.Context, who auth.Subject) (auth.Subject, error) { return who, nil })
	got, err := echo.Load(context.Background(), open(t, echo, auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"admin"}}))
	if err != nil {
		t.Fatalf("Load through an echoing resolver: %v", err)
	}
	if len(got.Roles) != 0 {
		t.Errorf("an echoing resolver answered the roles %v, which only the session held", got.Roles)
	}
}

func TestAnAccountThatLostItsTenantEndsTheSession(t *testing.T) {
	d := newDirectory(auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"member"}})
	s := resolvedStore(d)
	r := open(t, s, auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"member"}})

	// The resolver finds the id, in another tenant.
	d.mu.Lock()
	d.rows["acme/u-1"] = auth.Subject{ID: "u-1", Tenant: "globex", Roles: []string{"member"}}
	d.mu.Unlock()

	if _, err := s.Load(context.Background(), r); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("Load = %v, want session.ErrExpired for an account no longer in the session's tenant", err)
	}
	d.put(auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"member"}})
	if _, err := s.Load(context.Background(), r); !errors.Is(err, session.ErrExpired) {
		t.Errorf("Load = %v after the account came back, want the session to stay destroyed", err)
	}
}

func TestAResolverAnsweringForAnotherAccountEndsTheSession(t *testing.T) {
	s := hhttp.NewSessionStore(sessionAppKey, time.Hour, false, nil)
	s.ResolveSubjects(func(context.Context, auth.Subject) (auth.Subject, error) {
		return auth.Subject{ID: "u-2", Tenant: "acme", Roles: []string{"admin"}}, nil
	})
	if _, err := s.Load(context.Background(), open(t, s, auth.Subject{ID: "u-1", Tenant: "acme"})); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("Load = %v, want session.ErrExpired when the resolver answers for somebody else", err)
	}
}

func TestAFailingResolverFailsLoadAndNeverAnswersTheStoredCopy(t *testing.T) {
	d := newDirectory(auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"admin"}})
	s := resolvedStore(d)
	r := open(t, s, auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"admin"}})

	cause := errors.New("connection refused")
	d.mu.Lock()
	d.err = cause
	d.mu.Unlock()
	got, err := s.Load(context.Background(), r)
	if !errors.Is(err, hhttp.ErrSubjectUnresolved) || !errors.Is(err, cause) {
		t.Fatalf("Load = %v, want ErrSubjectUnresolved wrapping the resolver's error", err)
	}
	if got.ID != "" || got.HasRole("admin") {
		t.Errorf("Load answered %+v beside its error, want nothing", got)
	}

	// The failure is not a sign-out: the session is there once the account
	// can be read again.
	d.mu.Lock()
	d.err = nil
	d.mu.Unlock()
	if _, err := s.Load(context.Background(), r); err != nil {
		t.Errorf("Load after the resolver recovered = %v, want the session still open", err)
	}
}

// The operator viewing an account is read afresh too: an operator who lost
// the role that let them in, or the account itself, does not keep acting
// through a session opened before.
func TestTheOperatorViewingAnAccountIsReadAfreshToo(t *testing.T) {
	operator := auth.Subject{ID: "op-1", Tenant: "acme", Roles: []string{"support"}}
	customer := auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"member"}}
	d := newDirectory(operator, customer)
	s := resolvedStore(d)
	viewed, err := auth.ImpersonateActing(operator, customer)
	if err != nil {
		t.Fatal(err)
	}
	r := open(t, s, viewed)

	d.put(auth.Subject{ID: "op-1", Tenant: "acme", Roles: []string{"reader"}})
	got, err := s.Load(context.Background(), r)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Impersonator == nil || got.Impersonator.ID != "op-1" || got.Impersonator.HasRole("support") || !got.Impersonator.HasRole("reader") || !got.Acting {
		t.Fatalf("Load answered the operator %+v acting=%v, want op-1 with their current roles, still acting", got.Impersonator, got.Acting)
	}

	d.mu.Lock()
	delete(d.rows, "acme/op-1")
	d.mu.Unlock()
	if _, err := s.Load(context.Background(), r); !errors.Is(err, session.ErrExpired) {
		t.Errorf("Load = %v after the operator's account was removed, want the session ended", err)
	}
}

// A resolver cannot start or stop an impersonation: who is viewing whom is
// the session's.
func TestTheResolverCannotMakeASubjectAnImpersonation(t *testing.T) {
	s := hhttp.NewSessionStore(sessionAppKey, time.Hour, false, nil)
	s.ResolveSubjects(func(_ context.Context, who auth.Subject) (auth.Subject, error) {
		who.Impersonator = &auth.Subject{ID: "op-9", Tenant: who.Tenant}
		who.Acting, who.Remembered = true, true
		return who, nil
	})
	got, err := s.Load(context.Background(), open(t, s, auth.Subject{ID: "u-1", Tenant: "acme"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Impersonator != nil || got.Acting || got.Remembered {
		t.Errorf("Load answered %+v, want the session's own Impersonator, Acting and Remembered", got)
	}
}

// Without a resolver the store refuses what it could not keep current, at the
// moment it would write it, and a session that already holds it.
func TestWithoutAResolverRolesAndActionsAreRefused(t *testing.T) {
	ctx := context.Background()
	for name, sub := range map[string]auth.Subject{
		"roles":    {ID: "u-1", Tenant: "acme", Roles: []string{"admin"}},
		"actions":  {ID: "u-1", Tenant: "acme", Actions: []auth.Action{"invoice.view"}},
		"operator": {ID: "u-1", Tenant: "acme", Impersonator: &auth.Subject{ID: "op-1", Tenant: "acme", Roles: []string{"support"}}},
	} {
		t.Run(name, func(t *testing.T) {
			b := hhttp.NewMemoryBackend()
			s := hhttp.NewSessionStore(sessionAppKey, time.Hour, false, b)
			rec := httptest.NewRecorder()
			if _, err := s.Start(ctx, rec, sub); !errors.Is(err, hhttp.ErrNoSubjectResolver) {
				t.Errorf("Start = %v, want ErrNoSubjectResolver", err)
			}
			if len(rec.Result().Cookies()) != 0 {
				t.Error("Start wrote a cookie for a session it refused")
			}
			if _, err := s.Rotate(ctx, httptest.NewRecorder(), "", sub); !errors.Is(err, hhttp.ErrNoSubjectResolver) {
				t.Errorf("Rotate = %v, want ErrNoSubjectResolver", err)
			}

			// A session written before the store refused it -- by an older
			// binary -- is not answered either.
			if err := b.Put(ctx, "sid-old", sub, time.Hour); err != nil {
				t.Fatal(err)
			}
			signer := hhttp.NewSessionStore(sessionAppKey, time.Hour, false, nil)
			signer.ResolveSubjects(func(_ context.Context, who auth.Subject) (auth.Subject, error) { return who, nil })
			r := open(t, signer, auth.Subject{ID: "u-1", Tenant: "acme"})
			id := signer.IDFromRequest(r)
			if err := b.Put(ctx, id, sub, time.Hour); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Load(ctx, r); !errors.Is(err, hhttp.ErrNoSubjectResolver) {
				t.Errorf("Load of a stored session with %s = %v, want ErrNoSubjectResolver", name, err)
			}
		})
	}
}

func TestWithoutAResolverASubjectWithNoRolesWorksAsBefore(t *testing.T) {
	s := hhttp.NewSessionStore(sessionAppKey, time.Hour, false, nil)
	got, err := s.Load(context.Background(), open(t, s, auth.Subject{ID: "u-1", Tenant: "acme", Verified: true}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != "u-1" || got.Tenant != "acme" || !got.Verified {
		t.Errorf("Load = %+v, want the subject the session was opened with", got)
	}
}

// With a resolver the session keeps who, not what: the roles are not written,
// so nothing that reads the record behind the store's back finds them either.
func TestWithAResolverTheSessionKeepsTheIdentityAlone(t *testing.T) {
	ctx := context.Background()
	b := hhttp.NewMemoryBackend()
	s := hhttp.NewSessionStore(sessionAppKey, time.Hour, false, b)
	s.ResolveSubjects(newDirectory(auth.Subject{ID: "u-1", Tenant: "acme"}).resolve)
	r := open(t, s, auth.Subject{ID: "u-1", Tenant: "acme", Roles: []string{"admin"}, Actions: []auth.Action{"x.y"}})
	stored, err := b.Get(ctx, s.IDFromRequest(r))
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Roles) != 0 || len(stored.Actions) != 0 || stored.ID != "u-1" || stored.Tenant != "acme" {
		t.Errorf("the session record holds %+v, want the id and the tenant without roles or actions", stored)
	}
}

func TestResolveSubjectsRefusesNilAndASecondResolver(t *testing.T) {
	s := hhttp.NewSessionStore(sessionAppKey, time.Hour, false, nil)
	mustPanic(t, "a nil resolver", func() { s.ResolveSubjects(nil) })
	s.ResolveSubjects(newDirectory().resolve)
	mustPanic(t, "a second resolver", func() { s.ResolveSubjects(newDirectory().resolve) })
}

func mustPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s did not panic", what)
		}
	}()
	f()
}
