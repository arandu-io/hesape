package middleware_test

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/session/middleware"
)

// A connection held open past a sign-out does not bring the session back when
// it closes.
//
// A websocket handler returns when its connection closes. The session used to
// be finished then, so it was saved as it stood when the connection opened --
// after a sign-out in another tab had destroyed it, which put the signed-in
// record back under the id the browser still held. The session is now saved at
// the takeover, and the connection closing writes nothing.
func TestAConnectionHeldOpenDoesNotBringASignedOutSessionBack(t *testing.T) {
	m := middleware.NewStartSession(manager(session.Config{}), nil)

	signIn := httptest.NewRecorder()
	m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _ := middleware.Session(r.Context())
		s.Put("subject", "1")
	})).ServeHTTP(signIn, pageRequest(http.MethodPost, "/login"))
	c := sessionCookie(t, signIn, "arandu_session")

	taken := make(chan struct{})
	signedOut := make(chan struct{})
	finished := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, brw, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Errorf("hijack through the session writer: %v", err)
				close(taken)
				return
			}
			defer func() { _ = conn.Close() }()
			_, _ = brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			_ = brw.Flush()
			close(taken)
			<-signedOut
		})).ServeHTTP(w, r)
	}))
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = io.WriteString(conn, "GET /live HTTP/1.1\r\nHost: example.test\r\n"+
		"Cookie: "+c.Name+"="+c.Value+"\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the status line: %v", err)
	}
	if strings.TrimSpace(line) != "HTTP/1.1 101 Switching Protocols" {
		t.Fatalf("the client read %q", line)
	}
	<-taken

	// Another tab signs out while the connection is open.
	out := pageRequest(http.MethodPost, "/logout")
	out.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _ := middleware.Session(r.Context())
		if err := s.Invalidate(r.Context()); err != nil {
			t.Errorf("invalidate: %v", err)
		}
	})).ServeHTTP(httptest.NewRecorder(), out)

	close(signedOut)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the held connection never finished")
	}

	var subject any
	again := pageRequest(http.MethodGet, "/")
	again.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	m.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _ := middleware.Session(r.Context())
		subject = s.Get("subject")
	})).ServeHTTP(httptest.NewRecorder(), again)
	if subject != nil {
		t.Fatalf("the signed-out session came back when the connection closed: subject = %v", subject)
	}
}
