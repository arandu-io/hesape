package exception_test

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

	"github.com/arandu-io/hesape/exception"
)

// A RespondUsing callback that takes the connection over has answered the
// request, and nothing is written after it.
//
// The callback is handed a probe that records whether anything reached the
// client. Without Unwrap the takeover was refused outright; with Unwrap alone
// it went around the probe, which still reported nothing written, and the
// handler then wrote its page onto a connection that was no longer the
// server's -- net/http's "response.WriteHeader on hijacked connection".
func TestARespondUsingCallbackThatTakesTheConnectionIsTheAnswer(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	h.RespondUsing(func(w http.ResponseWriter, _ *http.Request, _ error) {
		conn, brw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack through the probe: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = brw.WriteString("HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		_ = brw.Flush()
	})

	var (
		mu     sync.Mutex
		errlog bytes.Buffer
		done   = make(chan struct{})
	)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		h.Render(w, r, exception.Abort(http.StatusNotFound, ""))
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
	_, _ = io.WriteString(conn, "GET /missing HTTP/1.1\r\nHost: example.test\r\n\r\n")
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the status line: %v", err)
	}
	if strings.TrimSpace(line) != "HTTP/1.1 503 Service Unavailable" {
		t.Fatalf("the client read %q, want the callback's answer", line)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(errlog.String(), "hijacked") {
		t.Errorf("the handler wrote to the connection after the callback took it:\n%s", errlog.String())
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
