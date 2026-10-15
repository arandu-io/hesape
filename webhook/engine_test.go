package webhook

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	httpclient "github.com/arandu-io/hesape/http/client"
)

// The engine's own rules, held without a database: the transport, the
// classification of an answer, the policy applied to it, and the signature of
// each dialect.

// stubResolver answers every host with the same addresses.
type stubResolver []netip.Addr

func (r stubResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r, nil
}

func TestThePublicDialerRefusesEveryAddressInsideTheNetwork(t *testing.T) {
	dialed := 0
	next := func(context.Context, string, string) (net.Conn, error) { dialed++; return nil, errors.New("dialed") }
	dial := publicDialer(next, stubResolver{})
	for _, address := range []string{
		"127.0.0.1:443", "10.0.0.1:443", "172.16.0.1:443", "192.168.1.1:443", "169.254.169.254:80",
		"100.64.0.1:443", "100.127.255.254:443", "0.0.0.0:443", "[::1]:443", "[fe80::1]:443", "[fc00::1]:443",
		"[::ffff:100.64.0.1]:443",
	} {
		if _, err := dial(context.Background(), "tcp", address); !errors.Is(err, httpclient.ErrInternalAddress) {
			t.Errorf("dialing %s = %v, want ErrInternalAddress", address, err)
		}
	}
	if dialed != 0 {
		t.Fatalf("a refused address was dialed %d times", dialed)
	}
}

func TestThePublicDialerRefusesANameWhenAnyOfItsAddressesIsRefused(t *testing.T) {
	var got []string
	next := func(_ context.Context, _ string, address string) (net.Conn, error) {
		got = append(got, address)
		return nil, errors.New("dialed")
	}
	mixed := publicDialer(next, stubResolver{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("100.64.1.1")})
	if _, err := mixed(context.Background(), "tcp", "hooks.example.test:443"); !errors.Is(err, httpclient.ErrInternalAddress) {
		t.Fatalf("a name resolving to a shared address = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a refused name was dialed at %v", got)
	}

	public := publicDialer(next, stubResolver{netip.MustParseAddr("93.184.216.34")})
	_, _ = public(context.Background(), "tcp", "hooks.example.test:443")
	if len(got) != 1 || got[0] != "93.184.216.34:443" {
		t.Fatalf("a public name was dialed at %v, want the checked address", got)
	}
}

func TestTheClientFollowsOnlyTheRedirectsThePolicyAllows(t *testing.T) {
	e := &engine{client: &http.Client{}, prefix: aranduPrefix, now: time.Now}
	next := mustRequest(t, "http://example.test/next")

	none := e.clientFor(Policy{MaxRedirects: 0})
	if err := none.CheckRedirect(next, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("a policy of no redirects followed one: %v", err)
	}
	two := e.clientFor(Policy{MaxRedirects: 2})
	if err := two.CheckRedirect(next, make([]*http.Request, 1)); err != nil {
		t.Fatalf("the first redirect was refused: %v", err)
	}
	if err := two.CheckRedirect(next, make([]*http.Request, 2)); !errors.Is(err, errRedirectLimit) {
		t.Fatalf("the third redirect = %v, want the limit", err)
	}
	httpsOnly := e.clientFor(Policy{MaxRedirects: 2, HTTPSOnly: true})
	if err := httpsOnly.CheckRedirect(next, make([]*http.Request, 1)); !errors.Is(err, httpclient.ErrUnsupportedScheme) {
		t.Fatalf("an https-only policy followed a redirect to http: %v", err)
	}
	if none.Timeout != 0 || e.client.CheckRedirect != nil {
		t.Fatal("clientFor changed the engine's own client, or left a timeout the attempt's context already carries")
	}
}

func TestTheEngineAppliesThePolicyToWhatAnAttemptCameTo(t *testing.T) {
	answer := func(status int, retryAfter string) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			out := response(request, status, "")
			out.Header.Set("Retry-After", retryAfter)
			return out, nil
		})}
	}
	e := &engine{client: &http.Client{}, prefix: aranduPrefix, now: time.Now}
	target := shot{id: "d-1", url: "https://hooks.example.test", dialect: DialectArandu, body: []byte(`{}`),
		keys: signing{secrets: []string{testSecret}}}
	policy := DefaultPolicy()

	r := e.attempt(context.Background(), &take{shot: target, policy: policy, client: answer(http.StatusTooManyRequests, "60")})
	if r.Permanent || r.RetryAfter != time.Minute || r.Code != "http_status_429" || r.StatusCode != 429 {
		t.Fatalf("a 429 under the default policy = %+v", r.verdict)
	}
	policy.HonorRetryAfter = false
	r = e.attempt(context.Background(), &take{shot: target, policy: policy, client: answer(http.StatusTooManyRequests, "60")})
	if r.RetryAfter != 0 {
		t.Fatalf("a policy that ignores Retry-After kept %s", r.RetryAfter)
	}
	r = e.attempt(context.Background(), &take{shot: target, policy: policy, client: answer(http.StatusBadRequest, "")})
	if !r.Permanent {
		t.Fatal("a 400 under RetryTransient is retried")
	}
	policy.Retry = RetryEveryFailure
	r = e.attempt(context.Background(), &take{shot: target, policy: policy, client: answer(http.StatusBadRequest, "")})
	if r.Permanent {
		t.Fatal("a 400 under RetryEveryFailure is not retried")
	}
	r = e.attempt(context.Background(), &take{policy: policy, refused: &verdict{Code: FailureEndpointDeleted, Permanent: true}})
	if !r.Permanent || r.Code != FailureEndpointDeleted {
		t.Fatalf("a deleted endpoint is retried under RetryEveryFailure: %+v", r.verdict)
	}
	r = e.attempt(context.Background(), &take{shot: target, policy: policy, client: answer(http.StatusNoContent, "")})
	if !r.delivered() || r.StatusCode != http.StatusNoContent {
		t.Fatalf("a 204 = %+v", r.verdict)
	}
}

func TestACanceledAttemptIsRetriedUnderItsOwnCode(t *testing.T) {
	v := classify(context.Canceled, 0, "")
	if v.Permanent || v.Code != FailureCanceled {
		t.Fatalf("a canceled attempt = %+v", v)
	}
	v = classify(httpclient.ErrResponseTooLarge, 0, "")
	if !v.Permanent || v.Code != FailureResponseTooLarge {
		t.Fatalf("an oversized answer = %+v", v)
	}
}

func TestEachDialectSignsWithItsOwnHeaders(t *testing.T) {
	at := time.Unix(1700000000, 0)
	body := []byte(`{"ok":true}`)

	arandu := http.Header{}
	if err := sign(arandu, shot{id: "delivery-1", dialect: DialectArandu, body: body, keys: signing{secrets: []string{testSecret}}}, at, "X-Acme", "agent"); err != nil {
		t.Fatal(err)
	}
	if arandu.Get("X-Acme-Signature") != Sign([]byte(testSecret), "1700000000", "delivery-1", body) ||
		arandu.Get("X-Acme-Timestamp") != "1700000000" || arandu.Get("X-Acme-Delivery-Id") != "delivery-1" ||
		arandu.Get("User-Agent") != "agent" || arandu.Get("Content-Type") != "application/json" {
		t.Fatalf("the Arandu dialect wrote %v", arandu)
	}

	hmacOnly := http.Header{}
	if err := sign(hmacOnly, shot{id: "delivery-1", dialect: DialectHMAC, body: body, signatureHeader: "Signature",
		keys: signing{secrets: []string{testSecret}}}, at, aranduPrefix, ""); err != nil {
		t.Fatal(err)
	}
	if hmacOnly.Get("Signature") != SignBody([]byte(testSecret), body) || hmacOnly.Get("X-Arandu-Signature") != "" || hmacOnly.Get("User-Agent") != "" {
		t.Fatalf("the HMAC dialect without Arandu headers wrote %v", hmacOnly)
	}
	both := http.Header{}
	if err := sign(both, shot{id: "delivery-1", dialect: DialectHMAC, body: body, signatureHeader: "Signature", aranduHeaders: true,
		keys: signing{secrets: []string{testSecret}}}, at, aranduPrefix, ""); err != nil {
		t.Fatal(err)
	}
	if both.Get("Signature") == "" || both.Get("X-Arandu-Signature") == "" {
		t.Fatalf("the HMAC dialect with Arandu headers wrote %v", both)
	}

	key := "whsec_" + "MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
	standard := http.Header{}
	if err := sign(standard, shot{id: "msg_1", dialect: DialectStandardWebhooks, body: body,
		keys: signing{secrets: []string{key, key}}}, at, aranduPrefix, ""); err != nil {
		t.Fatal(err)
	}
	if standard.Get(StandardHeaderID) != "msg_1" || standard.Get(StandardHeaderTimestamp) != "1700000000" ||
		len(strings.Fields(standard.Get(StandardHeaderSignature))) != 2 || standard.Get("X-Arandu-Signature") != "" {
		t.Fatalf("the Standard Webhooks dialect wrote %v", standard)
	}

	if err := sign(http.Header{}, shot{dialect: DialectArandu, keys: signing{secrets: []string{"short"}}}, at, aranduPrefix, ""); !errors.Is(err, ErrSecretTooShort) {
		t.Fatalf("a short secret signed: %v", err)
	}
}

func TestASealedValueOpensOnlyWhereItWasSealed(t *testing.T) {
	seal, err := newSealer([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := seal.seal("acme", "e-1", "secret", "the secret")
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := seal.open("acme", "e-1", "secret", sealed); err != nil || plain != "the secret" {
		t.Fatalf("open = %q, %v", plain, err)
	}
	for _, elsewhere := range [][3]string{{"globex", "e-1", "secret"}, {"acme", "e-2", "secret"}, {"acme", "e-1", "headers"}} {
		if _, err := seal.open(elsewhere[0], elsewhere[1], elsewhere[2], sealed); !errors.Is(err, errSealedElsewhere) {
			t.Errorf("a value sealed for acme/e-1/secret opened at %v: %v", elsewhere, err)
		}
	}
}

func TestAPolicyStoredOnAnEndpointReadsBackAsWritten(t *testing.T) {
	p := DefaultPolicy()
	p.Tries, p.Backoff, p.DisableAfter, p.HTTPSOnly = 4, []time.Duration{time.Second, 90 * time.Second}, 3, true
	encoded, err := encodePolicy(&p)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodePolicy(encoded)
	if err != nil || decoded == nil || decoded.Tries != 4 || decoded.DisableAfter != 3 || !decoded.HTTPSOnly ||
		len(decoded.Backoff) != 2 || decoded.Backoff[1] != 90*time.Second || decoded.Timeout != p.Timeout {
		t.Fatalf("decoded = %+v, %v", decoded, err)
	}
	if plan := p.plan(); len(plan) != 3 || plan[2] != 90*time.Second {
		t.Fatalf("plan = %v, want the last wait repeated up to Tries-1", plan)
	}
}

func mustRequest(t *testing.T, raw string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
