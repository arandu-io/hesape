package foundation

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
	"time"
)

// A connection the handler took over is not written to again on the way out.
//
// The recorder sends what it buffered when the handler returns, and a handler
// that hijacked has nothing buffered and nothing to send: writing a status then
// lands on a connection that is no longer the server's, and net/http reports it
// as "response.WriteHeader on hijacked connection" for every websocket opened
// in development, where this middleware is mounted. Before the recorder had
// Unwrap the takeover did not even get that far: the controller found nothing
// to follow and the handler was refused.
func TestAHijackedConnectionIsNotAnsweredAgainOnTheWayOut(t *testing.T) {
	reloadTag = []byte(`<script></script>`)
	t.Cleanup(func() { reloadTag = nil })

	var (
		mu     sync.Mutex
		errlog bytes.Buffer
		done   = make(chan struct{})
	)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		liveReload(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, brw, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Errorf("hijack through the recorder: %v", err)
				return
			}
			defer func() { _ = conn.Close() }()
			_, _ = brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			_ = brw.Flush()
		})).ServeHTTP(w, r)
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
	_, _ = io.WriteString(conn, "GET /app/key HTTP/1.1\r\nHost: example.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
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
		t.Errorf("the recorder wrote to the connection after the handler took it:\n%s", errlog.String())
	}
}

// A handler that holds a response open lifts the write deadline through the
// controller, and in development that controller meets this recorder first.
func TestTheWriteDeadlineIsReachableThroughTheRecorder(t *testing.T) {
	errc := make(chan error, 1)
	srv := httptest.NewServer(liveReload(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errc <- http.NewResponseController(w).SetWriteDeadline(time.Time{})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: ping\n\n")
	})))
	defer srv.Close()

	res, err := srv.Client().Get(srv.URL + "/stream")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_ = res.Body.Close()
	if err := <-errc; err != nil {
		t.Errorf("lifting the write deadline through the recorder: %v", err)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
