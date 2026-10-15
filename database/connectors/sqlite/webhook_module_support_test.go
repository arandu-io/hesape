package sqlite_test

// The webhook Module end to end on SQLite: the suites in the
// webhook_module_*_test.go files send, receive, retry, rotate and clean
// against a real database, a real queue worker and the Module's routes on
// routing.Router, the way an application wires them.
//
// They live in this module rather than beside the webhook package because
// they need a driver, and the root module carries none. What they leave to
// the framework is what only the framework has: its router, its session
// store and its sign-in redirect. The guard here admits a request carrying a
// cookie a test issued, and refuses any other with 401.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	sqliteconnector "github.com/arandu-io/hesape/database/connectors/sqlite"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/events"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/webhook"
)

const (
	tenant = "acme"
	// moduleDSN is the file database every world opens: WAL, so the worker
	// and the request handlers do not block each other, and an immediate
	// transaction lock, so a claim is decided at BEGIN.
	moduleDSN = "?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(20000)"
	// appKey seals the endpoints' secrets at rest.
	appKey = "0123456789abcdef0123456789abcdef"
	// sharedSecret is what a sender and this application's receiver share.
	sharedSecret = "shared-secret-shared-secret-0123"
	// sessionCookie is the cookie the test guard reads.
	sessionCookie = "webhook_test_session"
)

// world is an application with the module over a real database, the jobs
// table, a worker, and a router with a guard.
type world struct {
	db       *database.DB
	module   *webhook.Module
	sessions *sessions
	router   *routing.Router
	worker   *queue.Worker
	jobs     *queue.DatabaseQueue
	handled  *handled
	schema   migrations.Connection
}

// sessions are the subjects a test signed in, by the cookie value it issued.
type sessions struct {
	mu       sync.Mutex
	subjects map[string]auth.Subject
}

// guard admits a request whose cookie names a signed-in subject, with that
// subject on its context, and answers every other one 401.
func (s *sessions) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		s.mu.Lock()
		subject, ok := s.subjects[cookie.Value]
		s.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithSubject(r.Context(), subject)))
	})
}

// handled is what the test processors saw.
type handled struct {
	mu    sync.Mutex
	calls []*webhook.Call
	fail  error
}

func (h *handled) Process(_ context.Context, g auth.Grant, call *webhook.Call) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if auth.Tenant(g) != call.TenantID {
		panic("the processing Grant is not the call's tenant")
	}
	h.calls = append(h.calls, call)
	return h.fail
}

func (h *handled) seen() []*webhook.Call {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*webhook.Call(nil), h.calls...)
}

// install builds the world. configure adjusts the configuration after the
// test processor is set on every receiver that has none.
func install(t *testing.T, configure func(*webhook.Config, *handled)) *world {
	t.Helper()
	path := filepath.Join(t.TempDir(), "webhook.sqlite")
	handle, err := sql.Open(sqliteconnector.SQLiteConnector{}.DriverName(), "file:"+path+moduleDSN)
	if err != nil {
		t.Fatal(err)
	}
	handle.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = handle.Close() })
	connection := database.NewConnection(handle, path, "", map[string]any{"driver": string(database.DialectSQLite), "name": "webhook-test"})
	db := database.Wrap(handle, database.DialectSQLite)
	signedIn := &sessions{subjects: map[string]auth.Subject{}}

	h := &handled{}
	cfg := webhook.Config{Key: []byte(appKey)}
	if configure != nil {
		configure(&cfg, h)
	}
	for i := range cfg.Receivers {
		if cfg.Receivers[i].Process == nil && cfg.Receivers[i].ProcessSync == nil {
			cfg.Receivers[i].Process = h
		}
	}
	module, err := webhook.New(cfg, db, signedIn.guard)
	if err != nil {
		t.Fatal(err)
	}
	jobs := queue.NewDatabaseQueue(db)
	schema := database.ForMigrations(connection)
	for _, m := range append(append(jobs.Migrations(), events.NewModule().Migrations()...), module.Migrations()...) {
		if err := m.Up(context.Background(), schema); err != nil {
			t.Fatalf("applying %s: %v", m.GetName(), err)
		}
	}
	worker := queue.NewWorker(jobs, queue.WorkerOptions{Queue: webhook.DefaultQueue, Sleep: time.Millisecond})
	if err := module.RegisterJobHandlers(worker); err != nil {
		t.Fatal(err)
	}
	router := routing.NewRouter()
	module.Routes(router.ForModule(module.Name()))
	return &world{db: db, module: module, sessions: signedIn, router: router, worker: worker, jobs: jobs, handled: h, schema: schema}
}

// work runs the next job on the webhooks queue and answers the handler's
// error.
func (w *world) work(t *testing.T) error {
	t.Helper()
	return w.worker.RunNextJob(context.Background())
}

// pending is how many jobs wait on the webhooks queue.
func (w *world) pending(t *testing.T) int {
	t.Helper()
	n, err := w.jobs.Size(context.Background(), webhook.DefaultQueue)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// send makes one request to the router.
func (w *world) send(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	w.router.ServeHTTP(rec, req)
	return rec
}

// signIn answers the cookies of a session for the subject.
func (w *world) signIn(t *testing.T, subject auth.Subject) []*http.Cookie {
	t.Helper()
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	value := hex.EncodeToString(raw)
	w.sessions.mu.Lock()
	w.sessions.subjects[value] = subject
	w.sessions.mu.Unlock()
	return []*http.Cookie{{Name: sessionCookie, Value: value}}
}

// as makes a JSON request with the cookies.
func (w *world) as(t *testing.T, cookies []*http.Cookie, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Accept", "application/json")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return w.send(t, req)
}

// calls are the stored calls of the tenant, oldest first.
func (w *world) calls(t *testing.T, tenantID string) []*webhook.Call {
	t.Helper()
	rows, err := webhook.Calls(w.db).OrderBy("created_at").OrderBy("id").Get(context.Background(), auth.SystemGrant(webhook.CallReceive, tenantID))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// manager is a subject of the tenant given every action of the package.
func manager(tenantID string) auth.Subject {
	return auth.Subject{ID: "manager-" + tenantID, Tenant: tenantID, Actions: webhook.Actions(), Verified: true}
}

// httptestBody is a request body.
func httptestBody(body string) io.ReadCloser { return io.NopCloser(strings.NewReader(body)) }
