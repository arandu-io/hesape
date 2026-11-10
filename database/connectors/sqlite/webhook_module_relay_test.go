//go:build relay

package sqlite_test

// The relay of the webhook Module end to end on SQLite: a request received
// elsewhere is recorded with Module.Relay, attempted by the worker, retried,
// redelivered and cleaned, against a real database, a real queue and a real
// disk for the bodies.
//
// The file carries the relay build tag because this module is built against
// the release its go.mod pins, and Module.Relay is not in that release. Run it
// with the tag and a workspace that holds the checkout beside this module:
//
//	go work init . ../../.. && go test -tags relay ./...
//
// The release that carries Module.Relay is the one the next pin names, and the
// pin removes the tag from this file.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/events"
	"github.com/arandu-io/hesape/filesystem"
	"github.com/arandu-io/hesape/webhook"
)

// relayed is a request the old system took.
type relayed struct {
	method, uri, contentType string
	contentLength            int64
	chunked                  bool
	header                   http.Header
	body                     []byte
}

// oldSystem is the customer's server: it keeps what it is sent and answers
// what the test tells it to.
type oldSystem struct {
	mu     sync.Mutex
	got    []relayed
	status []int
	srv    *httptest.Server
}

// newOldSystem answers the statuses in order, then 200.
func newOldSystem(t *testing.T, status ...int) *oldSystem {
	t.Helper()
	o := &oldSystem{status: status}
	o.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		o.mu.Lock()
		o.got = append(o.got, relayed{
			method: r.Method, uri: r.RequestURI, contentType: r.Header.Get("Content-Type"), contentLength: r.ContentLength,
			chunked: len(r.TransferEncoding) > 0, header: r.Header.Clone(), body: body,
		})
		code := http.StatusOK
		if len(o.status) > 0 {
			code, o.status = o.status[0], o.status[1:]
		}
		o.mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(o.srv.Close)
	return o
}

func (o *oldSystem) requests() []relayed {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]relayed(nil), o.got...)
}

// relayWorld is a world that relays to the old system, with a local disk for
// the bodies and the fixed clock.
func relayWorld(t *testing.T, old *oldSystem, clk *clock, configure func(*webhook.Config)) (*world, *filesystem.Disk, string) {
	t.Helper()
	root := t.TempDir()
	adapter, err := filesystem.NewLocalFilesystemAdapter(root)
	if err != nil {
		t.Fatal(err)
	}
	disk := filesystem.NewDisk("bodies", adapter)
	w := install(t, func(c *webhook.Config, _ *handled) {
		c.Server.HTTPClient = old.srv.Client()
		c.Server.Now = clk.Now
		c.Server.Bodies = disk
		c.Server.RelayHeaders = []string{"X-Camera-Serial"}
		if configure != nil {
			configure(c)
		}
	})
	return w, disk, root
}

// relayEndpoint creates an endpoint of DialectRelay from its form and answers
// its id.
func relayEndpoint(t *testing.T, w *world, cookies []*http.Cookie, url string, keepPath bool, headers string) string {
	t.Helper()
	form := `{"name":"Old system","url":"` + url + `","dialect":"relay","active":true,"keep_path":` + map[bool]string{true: "true", false: "false"}[keepPath]
	if headers != "" {
		form += `,"headers":"` + headers + `"`
	}
	return createEndpoint(t, w, cookies, form+`}`).Data.ID
}

// plateMultipart is what a plate camera posts: a field and a JPEG of the size
// asked for.
func plateMultipart(t *testing.T, size int) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	m := multipart.NewWriter(&buf)
	if err := m.WriteField("plate", "ABC1D23"); err != nil {
		t.Fatal(err)
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="image"; filename="car.jpg"`)
	header.Set("Content-Type", "image/jpeg")
	part, err := m.CreatePart(header)
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
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), m.FormDataContentType()
}

// cameraHeaders are the headers a camera's request reaches this application
// with, behind a proxy: some travel on, most must not.
func cameraHeaders(contentType string) http.Header {
	return http.Header{
		"Content-Type": {contentType}, "User-Agent": {"HikvisionITS/2.1"}, "Accept": {"*/*"},
		"X-Camera-Serial": {"DS-TCG-0042"}, "X-Unlisted": {"no"},
		"Authorization": {"Basic Y2FtZXJhOnNlY3JldA=="}, "Cookie": {"s=1"}, "X-Forwarded-For": {"198.51.100.7"},
		"Cf-Connecting-Ip": {"198.51.100.7"}, "X-Real-Ip": {"198.51.100.7"}, "X-Api-Key": {"camera-key"},
	}
}

func relayGrant(tenantID string) auth.Grant {
	return auth.SystemGrant(webhook.DeliveryDispatch, tenantID)
}

func eventOf(id, name string, at time.Time) events.Stored {
	return events.Stored{ID: id, TenantID: tenant, Name: name, Payload: `{"order":"o-1"}`, OccurredAt: at}
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestARelayedRequestReachesTheOldServerExactlyAsItArrived(t *testing.T) {
	t.Parallel()
	clk := newClock(epoch)
	old := newOldSystem(t)
	w, disk, _ := relayWorld(t, old, clk, nil)
	cookies := w.signIn(t, manager(tenant))
	ctx := context.Background()
	endpoint := relayEndpoint(t, w, cookies, old.srv.URL+"/in", true, "X-Customer-Token: t-1\\nAuthorization: Bearer customer")

	body, contentType := plateMultipart(t, 3<<20)
	delivery, err := w.module.Relay(ctx, relayGrant(tenant), endpoint, webhook.RelayMessage{
		Method: http.MethodPost, Path: "/ISAPI/Traffic/events", Query: "n=1", ContentType: contentType,
		Header: cameraHeaders(contentType), Body: bytes.NewReader(body), Size: int64(len(body)),
	})
	if err != nil {
		t.Fatalf("relaying: %v", err)
	}
	if delivery.BodyKey == "" || delivery.Body != "" || delivery.BodySize != int64(len(body)) || delivery.BodySHA256 != sum(body) || !delivery.Relayed() {
		t.Fatalf("the recorded delivery is %+v, want a body of %d bytes kept on the disk and not in the row", delivery, len(body))
	}
	if n := w.pending(t); n != 1 {
		t.Fatalf("%d jobs wait after Relay, want its first attempt alone", n)
	}
	if len(old.requests()) != 0 {
		t.Fatal("Relay waited for the old system")
	}
	w.drain(t)

	got := old.requests()
	if len(got) != 1 {
		t.Fatalf("the old system took %d requests", len(got))
	}
	r := got[0]
	if !bytes.Equal(r.body, body) || r.contentType != contentType || r.method != http.MethodPost {
		t.Fatalf("the old system took %s %q with %d bytes, want the original %d bytes as %q", r.method, r.contentType, len(r.body), len(body), contentType)
	}
	if r.uri != "/in/ISAPI/Traffic/events?n=1" || r.chunked || r.contentLength != int64(len(body)) {
		t.Fatalf("the old system took %q chunked=%v length=%d", r.uri, r.chunked, r.contentLength)
	}
	for name := range r.header {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "signature") || strings.HasPrefix(lower, "x-arandu") || strings.HasPrefix(lower, "x-acme") ||
			strings.HasPrefix(lower, "webhook-") || strings.HasSuffix(lower, "delivery-id") {
			t.Errorf("the relayed request carries %s, which a signed delivery has", name)
		}
	}
	for name, want := range map[string]string{"User-Agent": "HikvisionITS/2.1", "Accept": "*/*", "X-Camera-Serial": "DS-TCG-0042", "X-Customer-Token": "t-1", "Authorization": "Bearer customer"} {
		if r.header.Get(name) != want {
			t.Errorf("%s is %q, want %q", name, r.header.Get(name), want)
		}
	}
	for _, name := range []string{"X-Unlisted", "Cookie", "X-Forwarded-For", "Cf-Connecting-Ip", "X-Real-Ip", "X-Api-Key"} {
		if r.header.Get(name) != "" {
			t.Errorf("%s = %q travelled to the old system", name, r.header.Get(name))
		}
	}
	if r.header.Get("Authorization") == "Basic Y2FtZXJhOnNlY3JldA==" {
		t.Error("the camera's credential travelled to the old system")
	}

	settled := w.delivery(t, delivery.ID)
	if settled.Status != webhook.DeliveryDelivered || settled.Attempts != 1 {
		t.Fatalf("the delivery is %s after %d attempts", settled.Status, settled.Attempts)
	}
	if exists, err := disk.Exists(ctx, relayGrant(tenant), delivery.BodyKey); err != nil || !exists {
		t.Fatalf("the body is not on the disk: %v %v", exists, err)
	}
	rec := w.as(t, cookies, http.MethodGet, "/webhook/deliveries/"+delivery.ID, "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), delivery.BodyKey) || !strings.Contains(rec.Body.String(), `"method":"POST"`) {
		t.Fatalf("the delivery answered %d %s", rec.Code, rec.Body.String())
	}
}

func TestASmallXMLBodyStaysInTheRowAndGoesToTheEndpointsOwnAddress(t *testing.T) {
	t.Parallel()
	clk := newClock(epoch)
	old := newOldSystem(t)
	w, disk, _ := relayWorld(t, old, clk, nil)
	cookies := w.signIn(t, manager(tenant))
	ctx := context.Background()
	endpoint := relayEndpoint(t, w, cookies, old.srv.URL+"/in", false, "")

	xml := []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><evento><placa>ABC1D23</placa><nota>\xe7\xe3o\x00</nota></evento>")
	delivery, err := w.module.Relay(ctx, relayGrant(tenant), endpoint, webhook.RelayMessage{
		Method: http.MethodPut, Path: "/ignored", ContentType: "text/xml; charset=ISO-8859-1", Body: bytes.NewReader(xml),
	})
	if err != nil {
		t.Fatal(err)
	}
	if delivery.BodyKey != "" || delivery.BodyEncoding != webhook.BodyBase64 || delivery.Size() != int64(len(xml)) {
		t.Fatalf("the recorded delivery is %+v, want the body in the row", delivery)
	}
	if keys, err := disk.AllFiles(ctx, relayGrant(tenant), ""); err != nil || len(keys) != 0 {
		t.Fatalf("the disk holds %v (%v) for a body that fits the row", keys, err)
	}
	w.drain(t)
	got := old.requests()
	if len(got) != 1 || !bytes.Equal(got[0].body, xml) || got[0].method != http.MethodPut || got[0].uri != "/in" ||
		got[0].contentType != "text/xml; charset=ISO-8859-1" {
		t.Fatalf("the old system took %+v, want the PUT of the original bytes to the endpoint's own address", got)
	}
}

func TestARetryAndARedeliveryStreamTheSameBytes(t *testing.T) {
	t.Parallel()
	clk := newClock(epoch)
	old := newOldSystem(t, http.StatusInternalServerError, http.StatusBadGateway)
	w, _, _ := relayWorld(t, old, clk, nil)
	cookies := w.signIn(t, manager(tenant))
	ctx := context.Background()
	endpoint := relayEndpoint(t, w, cookies, old.srv.URL+"/in", true, "")

	body, contentType := plateMultipart(t, 2<<20)
	delivery, err := w.module.Relay(ctx, relayGrant(tenant), endpoint, webhook.RelayMessage{
		Path: "/a", ContentType: contentType, Body: bytes.NewReader(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	// One job at a time: each attempt queues the next for the time the
	// module's clock says, and running it earlier only sends it back.
	for attempt := 1; attempt <= 3; attempt++ {
		if err := w.work(t); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if row := w.delivery(t, delivery.ID); attempt < 3 {
			if row.Status != webhook.DeliveryPending || row.NextAttemptAt == nil {
				t.Fatalf("after attempt %d the delivery is %s", attempt, row.Status)
			}
			clk.Set(*row.NextAttemptAt)
		}
	}
	if got := w.delivery(t, delivery.ID); got.Status != webhook.DeliveryDelivered || got.Attempts != 3 {
		t.Fatalf("the delivery is %s after %d attempts, want delivered on the third", got.Status, got.Attempts)
	}

	// A person sends it again from the panel.
	clk.Advance(time.Minute)
	rec := w.as(t, cookies, http.MethodPost, "/webhook/deliveries/"+delivery.ID+"/redeliver", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("the redelivery answered %d %s", rec.Code, rec.Body.String())
	}
	if err := w.work(t); err != nil {
		t.Fatal(err)
	}

	got := old.requests()
	if len(got) != 4 {
		t.Fatalf("the old system took %d requests, want three attempts and a redelivery", len(got))
	}
	for i, r := range got {
		if !bytes.Equal(r.body, body) || r.contentType != contentType || r.uri != "/in/a" || r.contentLength != int64(len(body)) || r.chunked {
			t.Errorf("request %d was %s %q of %d bytes (chunked=%v), want the original every time", i+1, r.uri, r.contentType, len(r.body), r.chunked)
		}
	}
	attempts, err := webhook.DeliveryAttempts(w.db).Where("delivery_id", "=", delivery.ID).OrderBy("attempt").Get(ctx, relayGrant(tenant))
	if err != nil || len(attempts) != 4 {
		t.Fatalf("the history holds %d attempts (%v), want 4", len(attempts), err)
	}
}

func TestACleanRemovesTheStoredBodyWithItsDeliveryAndTheOrphansOnlyWhenOld(t *testing.T) {
	t.Parallel()
	clk := newClock(time.Now().UTC())
	old := newOldSystem(t)
	w, disk, root := relayWorld(t, old, clk, nil)
	cookies := w.signIn(t, manager(tenant))
	ctx := context.Background()
	g := relayGrant(tenant)
	endpoint := relayEndpoint(t, w, cookies, old.srv.URL+"/in", true, "")

	big := bytes.Repeat([]byte("p"), 200<<10)
	settled, err := w.module.Relay(ctx, g, endpoint, webhook.RelayMessage{ContentType: "image/jpeg", Body: bytes.NewReader(big)})
	if err != nil {
		t.Fatal(err)
	}
	w.drain(t)
	pending, err := w.module.Relay(ctx, g, endpoint, webhook.RelayMessage{ContentType: "image/jpeg", Body: bytes.NewReader(big)})
	if err != nil {
		t.Fatal(err)
	}
	// Bodies a relay wrote and no delivery was recorded for: one old, one new.
	orphan, fresh := "webhook-relay/orphan-old", "webhook-relay/orphan-new"
	for _, key := range []string{orphan, fresh} {
		if err := disk.Put(ctx, g, key, strings.NewReader("lost"), "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
	}

	clk.Advance(webhook.DefaultRetention + time.Hour)
	// The disk dates the bodies by the module's clock, which has moved.
	for key, age := range map[string]time.Duration{orphan: 48 * time.Hour, fresh: time.Hour} {
		at := clk.Now().Add(-age)
		if err := os.Chtimes(filepath.Join(root, tenant, key), at, at); err != nil {
			t.Fatal(err)
		}
	}
	// The second delivery is pending past the retention too, which the clean
	// does not touch: it has not settled.
	if _, err := webhook.Deliveries(w.db).WhereKey(pending.ID).Update(ctx, g, map[string]any{"created_at": clk.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for _, task := range w.module.Schedule() {
		if err := task.Run(ctx, auth.Grant{}); err != nil {
			t.Fatalf("%s: %v", task.ID, err)
		}
	}

	exists := func(key string) bool {
		ok, err := disk.Exists(ctx, g, key)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if exists(settled.BodyKey) {
		t.Error("the body of a delivered delivery outlived it")
	}
	if row, err := webhook.Deliveries(w.db).WhereKey(settled.ID).First(ctx, g); err != nil || row != nil {
		t.Errorf("the delivered delivery is still there: %v %v", row, err)
	}
	if !exists(pending.BodyKey) {
		t.Error("the body of a delivery that has not settled was removed")
	}
	if exists(orphan) {
		t.Error("an orphaned body older than a day was kept")
	}
	if !exists(fresh) {
		t.Error("a body written moments ago was removed as an orphan")
	}
}

func TestAGivenUpDeliveryLosesItsBodyAtTheClean(t *testing.T) {
	t.Parallel()
	clk := newClock(time.Now().UTC())
	old := newOldSystem(t, http.StatusBadRequest)
	w, disk, _ := relayWorld(t, old, clk, nil)
	cookies := w.signIn(t, manager(tenant))
	ctx := context.Background()
	g := relayGrant(tenant)
	endpoint := relayEndpoint(t, w, cookies, old.srv.URL+"/in", false, "")

	delivery, err := w.module.Relay(ctx, g, endpoint, webhook.RelayMessage{Body: bytes.NewReader(bytes.Repeat([]byte("q"), 100<<10))})
	if err != nil {
		t.Fatal(err)
	}
	w.drain(t)
	if got := w.delivery(t, delivery.ID); got.Status != webhook.DeliveryFailed || !got.Permanent {
		t.Fatalf("a 400 left the delivery %s", got.Status)
	}
	clk.Advance(webhook.DefaultRetention + time.Hour)
	for _, task := range w.module.Schedule() {
		if err := task.Run(ctx, auth.Grant{}); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := disk.Exists(ctx, g, delivery.BodyKey); err != nil || ok {
		t.Fatalf("the body of a delivery that gave up is still on the disk: %v %v", ok, err)
	}
}

func TestARelayStaysInsideItsTenant(t *testing.T) {
	t.Parallel()
	clk := newClock(epoch)
	old := newOldSystem(t)
	w, disk, _ := relayWorld(t, old, clk, nil)
	acme := w.signIn(t, manager(tenant))
	ctx := context.Background()
	endpoint := relayEndpoint(t, w, acme, old.srv.URL+"/in", false, "")

	body := bytes.Repeat([]byte("z"), 300<<10)
	if _, err := w.module.Relay(ctx, relayGrant("globex"), endpoint, webhook.RelayMessage{Body: bytes.NewReader(body)}); !errors.Is(err, webhook.ErrNotFound) {
		t.Fatalf("another tenant relayed to acme's endpoint: %v", err)
	}
	if keys, err := disk.AllFiles(ctx, relayGrant("globex"), ""); err != nil || len(keys) != 0 {
		t.Fatalf("a refused relay left %v on the disk (%v)", keys, err)
	}
	if n := w.pending(t); n != 0 {
		t.Fatalf("%d jobs wait after a refused relay", n)
	}

	delivery, err := w.module.Relay(ctx, relayGrant(tenant), endpoint, webhook.RelayMessage{Body: bytes.NewReader(body)})
	if err != nil {
		t.Fatal(err)
	}
	if other, err := disk.Exists(ctx, relayGrant("globex"), delivery.BodyKey); err != nil || other {
		t.Fatalf("another tenant sees acme's body: %v %v", other, err)
	}
	globex := w.signIn(t, manager("globex"))
	if rec := w.as(t, globex, http.MethodGet, "/webhook/deliveries/"+delivery.ID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("another tenant read acme's delivery: %d", rec.Code)
	}
	if rec := w.as(t, globex, http.MethodPost, "/webhook/deliveries/"+delivery.ID+"/redeliver", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("another tenant redelivered acme's delivery: %d", rec.Code)
	}
}

func TestARelayToAPrivateAddressGivesUpAndSendsNothing(t *testing.T) {
	t.Parallel()
	old := newOldSystem(t) // on loopback
	root := t.TempDir()
	adapter, err := filesystem.NewLocalFilesystemAdapter(root)
	if err != nil {
		t.Fatal(err)
	}
	// No HTTPClient: the guarded transport, which refuses loopback.
	w := install(t, func(c *webhook.Config, _ *handled) { c.Server.Bodies = filesystem.NewDisk("bodies", adapter) })
	cookies := w.signIn(t, manager(tenant))
	ctx := context.Background()
	endpoint := relayEndpoint(t, w, cookies, old.srv.URL+"/in", false, "")

	delivery, err := w.module.Relay(ctx, relayGrant(tenant), endpoint, webhook.RelayMessage{Body: strings.NewReader("a plate")})
	if err != nil {
		t.Fatal(err)
	}
	w.drain(t)
	got := w.delivery(t, delivery.ID)
	if got.Status != webhook.DeliveryFailed || got.LastError == nil || *got.LastError != webhook.FailureUnsafeDestination || !got.Permanent {
		t.Fatalf("the delivery is %s with %v, want it given up as %s", got.Status, got.LastError, webhook.FailureUnsafeDestination)
	}
	if len(old.requests()) != 0 {
		t.Fatal("the private address was reached")
	}
}

func TestOnlyARelayEndpointTakesARelayedRequestAndItTakesNothingElse(t *testing.T) {
	t.Parallel()
	clk := newClock(epoch)
	old := newOldSystem(t)
	w, _, _ := relayWorld(t, old, clk, func(c *webhook.Config) { c.Server.Events = []string{"order.paid"} })
	cookies := w.signIn(t, manager(tenant))
	ctx := context.Background()
	g := relayGrant(tenant)
	signed := createEndpoint(t, w, cookies, `{"name":"Billing","url":"`+old.srv.URL+`/hooks","events":["order.paid"],"active":true}`).Data.ID
	relay := relayEndpoint(t, w, cookies, old.srv.URL+"/in", false, "")

	if _, err := w.module.Relay(ctx, g, signed, webhook.RelayMessage{Body: strings.NewReader("x")}); !errors.Is(err, webhook.ErrNotRelayEndpoint) {
		t.Fatalf("a relay to a signing endpoint = %v", err)
	}
	if _, err := w.module.Send(ctx, g, relay, webhook.Message{Event: "order.paid", Body: []byte(`{}`)}); !errors.Is(err, webhook.ErrRelayEndpoint) {
		t.Fatalf("Send to a relay endpoint = %v", err)
	}
	if rec := w.as(t, cookies, http.MethodPost, "/webhook/endpoints/"+relay+"/test", ""); rec.Code != http.StatusConflict {
		t.Fatalf("a test call to a relay endpoint answered %d %s", rec.Code, rec.Body.String())
	}
	stored := time.Now().UTC()
	if err := w.module.Publisher().Publish(ctx, eventOf("evt-1", "order.paid", stored)); err != nil {
		t.Fatal(err)
	}
	w.drain(t)
	got := old.requests()
	if len(got) != 1 || got[0].uri != "/hooks" {
		t.Fatalf("the event reached %d servers/paths %v, want the signed endpoint alone", len(got), got)
	}
	if rec := w.as(t, cookies, http.MethodGet, "/webhook/endpoints/"+relay, ""); !strings.Contains(rec.Body.String(), `"keep_path":false`) || !strings.Contains(rec.Body.String(), `"dialect":"relay"`) {
		t.Fatalf("the relay endpoint answered %s", rec.Body.String())
	}
	if rec := w.as(t, cookies, http.MethodPost, "/webhook/endpoints", `{"name":"x","url":"`+old.srv.URL+`","events":["order.paid"],"keep_path":true}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("keep_path on a signing endpoint answered %d", rec.Code)
	}
}

func TestARelayedEventIsRecordedOnce(t *testing.T) {
	t.Parallel()
	clk := newClock(epoch)
	old := newOldSystem(t)
	w, disk, _ := relayWorld(t, old, clk, nil)
	cookies := w.signIn(t, manager(tenant))
	ctx := context.Background()
	g := relayGrant(tenant)
	endpoint := relayEndpoint(t, w, cookies, old.srv.URL+"/in", false, "")

	body := bytes.Repeat([]byte("r"), 100<<10)
	first, err := w.module.Relay(ctx, g, endpoint, webhook.RelayMessage{EventID: "camera-event-1", Body: bytes.NewReader(body)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.module.Relay(ctx, g, endpoint, webhook.RelayMessage{EventID: "camera-event-1", Body: bytes.NewReader(body)})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.BodyKey != first.BodyKey {
		t.Fatalf("the repeat recorded %s over %s", second.ID, first.ID)
	}
	if keys, err := disk.AllFiles(ctx, g, ""); err != nil || len(keys) != 1 {
		t.Fatalf("the disk holds %v (%v), want the first body alone", keys, err)
	}
	w.drain(t)
	if got := old.requests(); len(got) != 1 {
		t.Fatalf("the old system took %d requests for one event", len(got))
	}
}

func TestARelayWithoutADiskRefusesABodyThatDoesNotFitTheRow(t *testing.T) {
	t.Parallel()
	old := newOldSystem(t)
	w := install(t, func(c *webhook.Config, _ *handled) { c.Server.HTTPClient = old.srv.Client() })
	cookies := w.signIn(t, manager(tenant))
	ctx := context.Background()
	endpoint := relayEndpoint(t, w, cookies, old.srv.URL+"/in", false, "")

	if _, err := w.module.Relay(ctx, relayGrant(tenant), endpoint, webhook.RelayMessage{Body: bytes.NewReader(make([]byte, webhook.DefaultRelayInlineBytes+1))}); !errors.Is(err, webhook.ErrNoBodyDisk) {
		t.Fatalf("a body above the inline room with no disk = %v", err)
	}
	if _, err := w.module.Relay(ctx, relayGrant(tenant), endpoint, webhook.RelayMessage{Body: bytes.NewReader(make([]byte, webhook.DefaultRelayInlineBytes))}); err != nil {
		t.Fatalf("a body at the inline room with no disk = %v", err)
	}
	if n := w.pending(t); n != 1 {
		t.Fatalf("%d jobs wait, want the one that was accepted", n)
	}
}

func TestARelayLeavesTheCallersTransactionInCharge(t *testing.T) {
	t.Parallel()
	clk := newClock(epoch)
	old := newOldSystem(t)
	w, _, _ := relayWorld(t, old, clk, nil)
	cookies := w.signIn(t, manager(tenant))
	ctx := context.Background()
	endpoint := relayEndpoint(t, w, cookies, old.srv.URL+"/in", false, "")

	rollback := errors.New("the caller changed its mind")
	err := database.Transaction(ctx, w.db, func(ctx context.Context) error {
		if _, err := w.module.Relay(ctx, relayGrant(tenant), endpoint, webhook.RelayMessage{Body: strings.NewReader("a plate")}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("the transaction answered %v", err)
	}
	if n := w.pending(t); n != 0 {
		t.Fatalf("%d jobs wait after the caller's transaction rolled back", n)
	}
	rows, err := webhook.Deliveries(w.db).Get(ctx, relayGrant(tenant))
	if err != nil || len(rows) != 0 {
		t.Fatalf("%d deliveries survived the rollback (%v)", len(rows), err)
	}
}
