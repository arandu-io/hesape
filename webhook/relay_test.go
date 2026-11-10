package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/filesystem"
)

// The relay's own rules, held without a database: what is forwarded and what
// is not, where a body is kept and how it is read back, and the request an
// attempt makes of it. The deliveries recorded in a database, retried and
// cleaned are proved on a real driver, in database/connectors/sqlite.

// relaySender is a sender with what the relay reads and nothing else: a local
// disk for the large bodies, 64 bytes of inline room, no database.
func relaySender(t *testing.T) (*sender, auth.Grant) {
	t.Helper()
	adapter, err := filesystem.NewLocalFilesystemAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &sender{
		bodies: filesystem.NewDisk("bodies", adapter), inline: 64, forwarded: map[string]bool{}, custom: "X-Acme",
		now: time.Now,
	}
	//arandu:system-grant a test of the relay's disk: the Grant is the tenant's, as the application's would be.
	return s, auth.SystemGrant(DeliveryDispatch, "acme")
}

// multipartOf is a multipart body of the size asked for: a text field and a
// part of random bytes, the shape of what a plate camera posts.
func multipartOf(t *testing.T, size int) (body []byte, contentType string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("plate", "ABC1D23"); err != nil {
		t.Fatal(err)
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="image"; filename="car.jpg"`)
	header.Set("Content-Type", "image/jpeg")
	part, err := w.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), w.FormDataContentType()
}

// capture is a server that keeps what it is sent.
type capture struct {
	mu   sync.Mutex
	got  []captured
	srv  *httptest.Server
	code int
}

type captured struct {
	method, uri, contentType string
	contentLength            int64
	chunked                  bool
	header                   http.Header
	body                     []byte
}

func newCapture(t *testing.T) *capture {
	t.Helper()
	c := &capture{code: http.StatusOK}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.got = append(c.got, captured{
			method: r.Method, uri: r.RequestURI, contentType: r.Header.Get("Content-Type"), contentLength: r.ContentLength,
			chunked: len(r.TransferEncoding) > 0, header: r.Header.Clone(), body: body,
		})
		code := c.code
		c.mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *capture) requests() []captured {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]captured(nil), c.got...)
}

// attemptRelayed makes one attempt of a relayed shot on a client that reaches
// the test server.
func attemptRelayed(t *testing.T, target shot) result {
	t.Helper()
	e := &engine{client: &http.Client{}, prefix: aranduPrefix, now: time.Now}
	return e.attempt(context.Background(), &take{shot: target, policy: DefaultPolicy()})
}

func TestAnInlineBodyIsSentByteForByteWithoutASignature(t *testing.T) {
	t.Parallel()

	server := newCapture(t)
	s, g := relaySender(t)
	body := []byte("<?xml version=\"1.0\"?><placa v=\"\xe9\x00\xff\">ABC1D23</placa>")
	item, err := s.spool(context.Background(), g, RelayMessage{Body: bytes.NewReader(body), Size: int64(len(body))}, MaxRelayBytes)
	if err != nil {
		t.Fatal(err)
	}
	if item.key != "" || !bytes.Equal(item.inline, body) {
		t.Fatalf("a body of %d bytes under the inline room of 64 was kept as %q", len(body), item.key)
	}
	sum := sha256.Sum256(body)
	if item.sha256 != hex.EncodeToString(sum[:]) || item.size != int64(len(body)) {
		t.Fatalf("the digest and the size of the body are %s and %d", item.sha256, item.size)
	}

	d := &Delivery{ID: "d-1", URL: server.srv.URL + "/lpr", Dialect: DialectRelay, Method: http.MethodPut,
		ContentType: "text/xml; charset=ISO-8859-1", BodySize: item.size, BodyEncoding: BodyBase64,
		Body: base64.StdEncoding.EncodeToString(item.inline)}
	target, refused := s.prepareRelay(g, d, DefaultPolicy())
	if refused != nil {
		t.Fatalf("the delivery was refused: %+v", refused)
	}
	if r := attemptRelayed(t, target); !r.delivered() {
		t.Fatalf("the attempt came to %+v", r.verdict)
	}
	got := server.requests()
	if len(got) != 1 {
		t.Fatalf("the server took %d requests", len(got))
	}
	r := got[0]
	if r.method != http.MethodPut || r.contentType != "text/xml; charset=ISO-8859-1" || !bytes.Equal(r.body, body) {
		t.Fatalf("the server took %s %q with %d bytes, want the original request", r.method, r.contentType, len(r.body))
	}
	for name := range r.header {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "signature") || strings.HasPrefix(lower, "x-arandu") || strings.HasPrefix(lower, "webhook-") ||
			strings.HasSuffix(lower, "-delivery-id") || strings.HasSuffix(lower, "-timestamp") {
			t.Errorf("the relayed request carries %s, which is the signing of a delivery", name)
		}
	}
}

func TestALargeMultipartBodyIsKeptOnTheDiskAndStreamedIdenticalOnEveryAttempt(t *testing.T) {
	t.Parallel()

	server := newCapture(t)
	s, g := relaySender(t)
	body, contentType := multipartOf(t, 5<<20)
	item, err := s.spool(context.Background(), g, RelayMessage{Body: bytes.NewReader(body), Size: int64(len(body))}, MaxRelayBytes)
	if err != nil {
		t.Fatal(err)
	}
	if item.key == "" || !strings.HasPrefix(item.key, bodyDirectory) || item.inline != nil {
		t.Fatalf("a body of %d bytes was kept as %+v, want the disk", len(body), item)
	}
	sum := sha256.Sum256(body)
	if item.sha256 != hex.EncodeToString(sum[:]) || item.size != int64(len(body)) {
		t.Fatalf("the digest and the size of the stored body are %s and %d", item.sha256, item.size)
	}

	d := &Delivery{ID: "d-1", URL: server.srv.URL + "/lpr", Dialect: DialectRelay, Method: http.MethodPost,
		ContentType: contentType, BodySize: item.size, BodyKey: item.key}
	target, refused := s.prepareRelay(g, d, DefaultPolicy())
	if refused != nil {
		t.Fatalf("the delivery was refused: %+v", refused)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if r := attemptRelayed(t, target); !r.delivered() {
			t.Fatalf("attempt %d came to %+v", attempt, r.verdict)
		}
	}
	got := server.requests()
	if len(got) != 2 {
		t.Fatalf("the server took %d requests, want one per attempt", len(got))
	}
	for i, r := range got {
		if !bytes.Equal(r.body, body) || r.contentType != contentType {
			t.Errorf("attempt %d sent %d bytes as %q, want the %d of the original as %q", i+1, len(r.body), r.contentType, len(body), contentType)
		}
		if r.chunked || r.contentLength != int64(len(body)) {
			t.Errorf("attempt %d went chunked=%v with a length of %d, want a plain request of %d", i+1, r.chunked, r.contentLength, len(body))
		}
	}
}

func TestABodyOnTheDiskIsReadBackOnlyForItsOwnTenant(t *testing.T) {
	t.Parallel()

	s, g := relaySender(t)
	item, err := s.spool(context.Background(), g, RelayMessage{Body: strings.NewReader(strings.Repeat("x", 500))}, MaxRelayBytes)
	if err != nil {
		t.Fatal(err)
	}
	//arandu:system-grant a test of tenant isolation: another tenant's Grant, asking for the first one's key.
	other := auth.SystemGrant(DeliveryDispatch, "globex")
	_, open, _ := s.relayBody(other, &Delivery{BodyKey: item.key})
	if _, err := open(context.Background()); !errors.Is(err, filesystem.ErrNotFound) {
		t.Fatalf("another tenant read the body: %v", err)
	}
	if v := bodyVerdict(errors.Join(filesystem.ErrNotFound)); v.Code != FailureBodyMissing || !v.Permanent {
		t.Fatalf("a body that is not there came to %+v, want it permanent", v)
	}
	if v := bodyVerdict(errors.New("r2 is down")); v.Code != FailureBodyUnavailable || v.Permanent {
		t.Fatalf("a disk that did not answer came to %+v, want another attempt", v)
	}
}

func TestSpoolRefusesWhatItCannotKeep(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, g := relaySender(t)
	s.bodies = nil
	if _, err := s.spool(ctx, g, RelayMessage{Body: strings.NewReader(strings.Repeat("x", 65))}, MaxRelayBytes); !errors.Is(err, ErrNoBodyDisk) {
		t.Fatalf("a body above the inline room with no disk = %v, want ErrNoBodyDisk", err)
	}
	if item, err := s.spool(ctx, g, RelayMessage{Body: strings.NewReader(strings.Repeat("x", 64))}, MaxRelayBytes); err != nil || item.key != "" {
		t.Fatalf("a body at the inline room with no disk = %+v, %v, want it kept in the row", item, err)
	}

	s, g = relaySender(t)
	if _, err := s.spool(ctx, g, RelayMessage{Body: strings.NewReader(strings.Repeat("x", 1000))}, 999); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("a body above the policy = %v, want ErrPayloadTooLarge", err)
	}
	if _, err := s.spool(ctx, g, RelayMessage{Body: strings.NewReader(strings.Repeat("x", 30))}, 29); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("an inline body above the policy = %v, want ErrPayloadTooLarge", err)
	}
	if _, err := s.spool(ctx, g, RelayMessage{Body: strings.NewReader(strings.Repeat("x", 1000)), Size: 999}, MaxRelayBytes); !errors.Is(err, ErrBodySize) {
		t.Fatalf("a body that is not the length announced = %v, want ErrBodySize", err)
	}
	if _, err := s.spool(ctx, g, RelayMessage{Body: strings.NewReader("short"), Size: 9}, MaxRelayBytes); !errors.Is(err, ErrBodySize) {
		t.Fatalf("an inline body that is not the length announced = %v, want ErrBodySize", err)
	}
	// What was refused after it was written is not left behind.
	keys, err := s.bodies.AllFiles(ctx, g, bodyDirectory)
	if err != nil || len(keys) != 0 {
		t.Fatalf("the disk holds %v (%v) after every body was refused", keys, err)
	}
}

func TestSpoolReadsNoMoreThanThePolicyAllows(t *testing.T) {
	t.Parallel()

	s, g := relaySender(t)
	read := &countingReader{r: neverEnding{}}
	_, err := s.spool(context.Background(), g, RelayMessage{Body: read}, 4096)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("a body with no end = %v, want ErrPayloadTooLarge", err)
	}
	if read.n > 4096+64+1+(1<<16) {
		t.Fatalf("%d bytes were read of a body the policy caps at 4096", read.n)
	}
}

// neverEnding is a body with no end.
type neverEnding struct{}

func (neverEnding) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestARelayedAttemptFollowsOnlyTheRedirectsThatKeepTheRequest(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var seen []string
	var bodies [][]byte
	mux := http.NewServeMux()
	mux.HandleFunc("/found", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/temporary", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		bodies = append(bodies, body)
		mu.Unlock()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	s, g := relaySender(t)
	payload := bytes.Repeat([]byte("0123456789"), 100)
	item, err := s.spool(context.Background(), g, RelayMessage{Body: bytes.NewReader(payload)}, MaxRelayBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/found", "/temporary"} {
		d := &Delivery{ID: "d-1", URL: srv.URL + path, Dialect: DialectRelay, Method: http.MethodPost, ContentType: "application/octet-stream",
			BodySize: item.size, BodyKey: item.key}
		target, refused := s.prepareRelay(g, d, DefaultPolicy())
		if refused != nil {
			t.Fatal(refused)
		}
		r := attemptRelayed(t, target)
		if path == "/found" && (r.delivered() || !r.Permanent) {
			t.Fatalf("a 302 of a relayed POST came to %+v, want it a permanent failure: the redirect would send a GET with no body", r.verdict)
		}
		if path == "/temporary" && !r.delivered() {
			t.Fatalf("a 307 of a relayed POST came to %+v", r.verdict)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != "POST /final" || !bytes.Equal(bodies[0], payload) {
		t.Fatalf("the redirect target saw %v with %d bytes, want the one POST that kept its body", seen, len(bodies[0]))
	}
}

func TestARelayedAttemptRefusesAPrivateDestinationOnTheGuardedClient(t *testing.T) {
	t.Parallel()

	server := newCapture(t) // listens on loopback
	e, err := newEngine(nil, aranduPrefix, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	target := shot{id: "d-1", url: server.srv.URL, dialect: DialectRelay, method: http.MethodPost, body: []byte("x")}
	r := e.attempt(context.Background(), &take{shot: target, policy: DefaultPolicy()})
	if r.Code != FailureUnsafeDestination || !r.Permanent {
		t.Fatalf("a relay to a loopback address came to %+v, want %s and permanent", r.verdict, FailureUnsafeDestination)
	}
	if len(server.requests()) != 0 {
		t.Fatal("the loopback server was reached")
	}
}

func TestAMissingBodyOnTheDiskFailsTheAttemptForGood(t *testing.T) {
	t.Parallel()

	s, g := relaySender(t)
	d := &Delivery{ID: "d-1", URL: "https://hooks.example.test", Dialect: DialectRelay, BodySize: 10, BodyKey: bodyDirectory + "gone"}
	target, refused := s.prepareRelay(g, d, DefaultPolicy())
	if refused != nil {
		t.Fatal(refused)
	}
	r := attemptRelayed(t, target)
	if r.Code != FailureBodyMissing || !r.Permanent {
		t.Fatalf("a body that is gone came to %+v", r.verdict)
	}
	if _, refused := s.prepareRelay(g, &Delivery{ID: "d-2", Dialect: DialectRelay, BodySize: MaxRelayBytes + 1, BodyKey: "k"}, DefaultPolicy()); refused == nil || refused.Code != FailureRequestTooLarge {
		t.Fatalf("a body above the policy at the attempt = %+v, want %s", refused, FailureRequestTooLarge)
	}
}

func TestOnlyTheAllowlistedHeadersAreForwardedAndTheEndpointsOwnWin(t *testing.T) {
	t.Parallel()

	s := &sender{custom: "X-Acme", forwarded: map[string]bool{"X-Camera-Serial": true}}
	own := map[string]string{"Accept": "application/json", "Authorization": "Bearer customer-token"}
	original := http.Header{
		"Content-Type":       {"image/jpeg"},
		"Content-Encoding":   {"gzip"},
		"Accept":             {"*/*"},
		"User-Agent":         {"HikvisionITS/1.0"},
		"X-Camera-Serial":    {"DS-123"},
		"Authorization":      {"Basic Y2FtZXJhOnNlY3JldA=="},
		"Cookie":             {"session=1"},
		"Host":               {"alpr.sh"},
		"X-Forwarded-For":    {"10.0.0.1"},
		"Cf-Connecting-Ip":   {"203.0.113.9"},
		"X-Real-Ip":          {"203.0.113.9"},
		"X-Api-Key":          {"k"},
		"X-Arandu-Signature": {"sha256=00"},
		"X-Acme-Timestamp":   {"1"},
		"X-Unlisted":         {"nope"},
		"Content-Length":     {"99"},
		"Bad":                {"line\r\nbreak"},
	}
	if err := s.forward(own, original); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Content-Encoding": "gzip", "Accept": "application/json", "User-Agent": "HikvisionITS/1.0",
		"X-Camera-Serial": "DS-123", "Authorization": "Bearer customer-token",
	}
	if len(own) != len(want) {
		t.Fatalf("the headers sent are %v, want %v", own, want)
	}
	for name, value := range want {
		if own[name] != value {
			t.Errorf("%s is %q, want %q", name, own[name], value)
		}
	}
}

func TestForbiddenRelayHeaders(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"Authorization", "authorization", "Proxy-Authorization", "Cookie", "Set-Cookie", "Host", "Content-Length",
		"Transfer-Encoding", "Connection", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip", "True-Client-Ip", "CF-Ray",
		"Cf-Connecting-Ip", "X-Arandu-Signature", "Webhook-Signature", "X-Api-Key", "X-Auth-Token", "Content-Type", "Via"} {
		if !forbiddenRelayHeader(name) {
			t.Errorf("%s may be forwarded", name)
		}
	}
	for _, name := range []string{"Accept", "User-Agent", "Content-Encoding", "X-Camera-Serial", "Soapaction"} {
		if forbiddenRelayHeader(name) {
			t.Errorf("%s is forbidden", name)
		}
	}
}

func TestRelayURLKeepsThePathOnlyWhenAskedAndNeverClimbsOutOfTheEndpoint(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name, base, path, query string
		keep                    bool
		want                    string
	}{
		{"not kept", "https://old.example.test/in", "/ISAPI/x", "a=1", false, "https://old.example.test/in"},
		{"kept", "https://old.example.test/in", "/ISAPI/x", "a=1", true, "https://old.example.test/in/ISAPI/x?a=1"},
		{"kept on a bare host", "https://old.example.test", "/ISAPI/x", "", true, "https://old.example.test/ISAPI/x"},
		{"trailing slash", "https://old.example.test/in/", "/a/b/", "", true, "https://old.example.test/in/a/b/"},
		{"query joined", "https://old.example.test/in?token=t", "/a", "n=2", true, "https://old.example.test/in/a?token=t&n=2"},
		{"nothing to append", "https://old.example.test/in", "", "", true, "https://old.example.test/in"},
		{"escaped", "https://old.example.test/in", "/a b/ç", "", true, "https://old.example.test/in/a%20b/%C3%A7"},
	} {
		got, err := relayURL(c.base, c.keep, c.path, c.query)
		if err != nil || got != c.want {
			t.Errorf("%s: relayURL = %q, %v, want %q", c.name, got, err, c.want)
		}
	}
	for _, c := range [][2]string{{"/../../admin", ""}, {"/a/../b", ""}, {"/a\r\nHost: x", ""}, {"/a", "x=1 y=2"}, {"/a", "x=1#f"}, {"/a", "x\n=1"}} {
		if got, err := relayURL("https://old.example.test/in", true, c[0], c[1]); !errors.Is(err, ErrInvalidMessage) {
			t.Errorf("relayURL(%q, %q) = %q, %v, want ErrInvalidMessage", c[0], c[1], got, err)
		}
	}
	if got, err := relayURL("https://old.example.test/in", true, "//evil.example.test/x", ""); err != nil || !strings.HasPrefix(got, "https://old.example.test/in/") {
		t.Errorf("a path that looks like an authority = %q, %v, want it kept under the endpoint's host", got, err)
	}
}

func TestARelayedRequestLineIsValidated(t *testing.T) {
	t.Parallel()

	body := strings.NewReader("x")
	for _, msg := range []RelayMessage{
		{Method: "DELETE", Body: body}, {Method: "GET", Body: body}, {Body: nil}, {Body: body, Size: -1},
		{Body: body, ContentType: "text/plain\r\nX-Evil: 1"}, {Body: body, ContentType: strings.Repeat("a", 300)},
	} {
		if _, _, err := relayRequestLine(msg); !errors.Is(err, ErrInvalidMessage) {
			t.Errorf("relayRequestLine(%+v) = %v, want ErrInvalidMessage", msg, err)
		}
	}
	for method, want := range map[string]string{"": "POST", "post": "POST", "PUT": "PUT", " put ": "PUT"} {
		got, _, err := relayRequestLine(RelayMessage{Method: method, Body: body})
		if err != nil || got != want {
			t.Errorf("relayRequestLine(method %q) = %q, %v, want %q", method, got, err, want)
		}
	}
}

func TestOnlyARelayEndpointTakesRelayedRequestsAndItTakesNothingElse(t *testing.T) {
	t.Parallel()

	endpoint := Endpoint{Dialect: DialectRelay, Active: true, Events: AllEvents}
	if endpoint.Receives("order.paid") {
		t.Fatal("an endpoint of DialectRelay receives the events of the outbox")
	}
	endpoint.Dialect = DialectArandu
	if !endpoint.Receives("order.paid") {
		t.Fatal("an endpoint subscribed to every event receives none")
	}
}

func TestTheRelayLimitsAreValidated(t *testing.T) {
	t.Parallel()

	good := Config{Key: []byte(testSecret)}
	for _, bad := range []Config{
		{Key: good.Key, Server: Server{RelayInlineBytes: -1}},
		{Key: good.Key, Server: Server{RelayInlineBytes: MaxRelayInlineBytes + 1}},
		{Key: good.Key, Server: Server{RelayHeaders: []string{"Authorization"}}},
		{Key: good.Key, Server: Server{RelayHeaders: []string{"X-Forwarded-For"}}},
		{Key: good.Key, Server: Server{RelayHeaders: []string{"not a header"}}},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("Validate accepted %+v", bad.Server)
		}
	}
	if err := (Config{Key: good.Key, Server: Server{RelayInlineBytes: MaxRelayInlineBytes, RelayHeaders: []string{"X-Camera-Serial"}}}).Validate(); err != nil {
		t.Errorf("Validate refused the ceiling: %v", err)
	}
	policy := DefaultPolicy()
	if policy.MaxRelayBytes != MaxRelayBytes || policy.MaxRequestBytes != MaxRequestBytes || policy.Validate() != nil {
		t.Fatalf("the default policy relays %d and sends %d", policy.MaxRelayBytes, policy.MaxRequestBytes)
	}
	policy.MaxRelayBytes = MaxRelayBytes + 1
	if policy.Validate() == nil {
		t.Error("a policy relaying above the ceiling was accepted")
	}
	policy.MaxRelayBytes = -1
	if policy.Validate() == nil {
		t.Error("a negative relay limit was accepted")
	}
	own := DefaultPolicy()
	own.MaxRelayBytes = 1234
	stored, err := encodePolicy(&own)
	if err != nil {
		t.Fatal(err)
	}
	back, err := decodePolicy(stored)
	if err != nil || back.MaxRelayBytes != 1234 {
		t.Fatalf("an endpoint's own policy came back as %+v, %v", back, err)
	}
	old, err := decodePolicy(`{"tries":3,"backoff_ms":[1000],"max_request_bytes":100,"timeout_ms":1000}`)
	if err != nil || old.MaxRelayBytes != 0 {
		t.Fatalf("a policy stored before the relay came back as %+v, %v, want it relaying nothing", old, err)
	}
}

func TestTheRelayMigrationsAreAdditiveAndRenderOnEveryEngine(t *testing.T) {
	t.Parallel()

	for _, m := range (&Module{}).Migrations() {
		name := m.GetName()
		if !strings.Contains(name, "keep_path") && !strings.Contains(name, "relay") {
			continue
		}
		statements := strings.ToLower(strings.Join(pretend(t, m), "\n"))
		for _, destructive := range []string{"drop ", "delete ", "update ", "rename "} {
			if strings.Contains(statements, destructive) {
				t.Errorf("%s renders %q: %s", name, destructive, statements)
			}
		}
		if !strings.Contains(statements, "alter table") {
			t.Errorf("%s does not alter a table: %s", name, statements)
		}
	}
	rendered := strings.ToLower(strings.Join(pretend(t, addDeliveryRelay{}), "\n"))
	for _, column := range []string{"method", "content_type", "body_size", "body_key", "body_encoding", deliveriesBodyIndex} {
		if !strings.Contains(rendered, column) {
			t.Errorf("addDeliveryRelay leaves out %s: %s", column, rendered)
		}
	}
	if !strings.Contains(strings.ToLower(strings.Join(pretend(t, addEndpointKeepPath{}), "\n")), "keep_path") {
		t.Error("addEndpointKeepPath leaves out keep_path")
	}
}

func TestADeliverySaysItsSizeWhicheverReleaseRecordedIt(t *testing.T) {
	t.Parallel()

	if got := (Delivery{Body: `{"a":1}`}).Size(); got != 7 {
		t.Errorf("a delivery recorded before the column existed says %d, want its 7 bytes", got)
	}
	if got := (Delivery{Body: "AAAA", BodySize: 3, BodyEncoding: BodyBase64}).Size(); got != 3 {
		t.Errorf("a relayed body in the row says %d, want the 3 bytes it decodes to", got)
	}
	if got := (Delivery{BodyKey: "k", BodySize: 9 << 20}).Size(); got != 9<<20 {
		t.Errorf("a relayed body on the disk says %d", got)
	}
	if got := (Delivery{Dialect: DialectRelay, BodyEncoding: BodyBase64}).Size(); got != 0 {
		t.Errorf("an empty relayed body says %d", got)
	}
}

func TestAPartialUploadLeavesNoBodyBehind(t *testing.T) {
	t.Parallel()

	s, g := relaySender(t)
	failing := io.MultiReader(strings.NewReader(strings.Repeat("x", 500)), errReader{})
	if _, err := s.spool(context.Background(), g, RelayMessage{Body: failing}, MaxRelayBytes); err == nil {
		t.Fatal("a body that failed half way was accepted")
	}
	keys, err := s.bodies.AllFiles(context.Background(), g, bodyDirectory)
	if err != nil || len(keys) != 0 {
		t.Fatalf("the disk holds %v (%v) after the body failed", keys, err)
	}
	if entries, _ := os.ReadDir(filepath.Join(s.bodies.GetAdapter().(*filesystem.LocalFilesystemAdapter).Root(), "acme", "webhook-relay")); len(entries) != 0 {
		t.Fatalf("a partial file is left: %v", entries)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, fmt.Errorf("the connection dropped") }
