package middleware_test

import (
	"bufio"
	"bytes"
	"io"
	stdlog "log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/cookie"
	"github.com/arandu-io/hesape/cookie/middleware"
)

// A connection can be taken over through both cookie writers, and neither
// writes anything once it was.
//
// Neither has a Hijack of its own, and neither needs one: on the way out they
// only rewrite the header map, which a taken connection never sends. What they
// must have is Unwrap, because without it http.ResponseController finds
// nothing to follow and refuses the takeover.
func TestAConnectionCanBeTakenOverThroughTheCookieWriters(t *testing.T) {
	t.Cleanup(middleware.FlushState)

	jar := cookie.NewCookieJar()
	queue := middleware.NewAddQueuedCookiesToResponse(jar)
	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))

	var (
		mu     sync.Mutex
		errlog bytes.Buffer
		done   = make(chan struct{})
	)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie.CookieJarFrom(r.Context()).Queue(&http.Cookie{Name: "theme", Value: "dark"})
		conn, brw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack through the cookie writers: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = brw.Flush()
	})
	chain := encrypt.Handle(queue.Handle(inner))
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		chain.ServeHTTP(w, r)
	}))
	srv.Config.ErrorLog = stdlog.New(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return errlog.Write(p)
	}), "", 0)
	srv.Start()
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = io.WriteString(conn, "GET /live HTTP/1.1\r\nHost: example.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the status line: %v", err)
	}
	if strings.TrimSpace(line) != "HTTP/1.1 101 Switching Protocols" {
		t.Fatalf("the client read %q", line)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(errlog.String(), "hijacked") {
		t.Errorf("a cookie writer wrote to the connection after the handler took it:\n%s", errlog.String())
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
