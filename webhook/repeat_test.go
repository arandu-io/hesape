package webhook

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestTheDeliveryKeyIsTheReceiversOwnOrTheSignedID: a repeat is known by the
// key the receiver declares when it declares one, else by the delivery id the
// validator's signature covered, else not at all.
func TestTheDeliveryKeyIsTheReceiversOwnOrTheSignedID(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/webhooks/x", strings.NewReader(`{"id":"evt_1"}`))
	r.Header.Set("X-Arandu-Delivery-ID", "dlv-1")
	r.Header.Set("X-Acme-Delivery-ID", "dlv-acme")
	r.Header.Set(StandardHeaderID, "msg_1")
	byBody := func(_ *http.Request, body []byte) string {
		if strings.Contains(string(body), "evt_1") {
			return "evt_1"
		}
		return ""
	}

	for name, c := range map[string]struct {
		rc   receiver
		v    SignatureValidator
		want string
	}{
		"arandu":                   {receiver{}, Arandu{}, "dlv-1"},
		"arandu under a prefix":    {receiver{}, Arandu{HeaderPrefix: "X-Acme"}, "dlv-acme"},
		"standard webhooks":        {receiver{}, StandardWebhooks{}, "msg_1"},
		"a body-only HMAC":         {receiver{}, HMAC{}, ""},
		"a shared header secret":   {receiver{}, HeaderSecret{Header: "X-Secret"}, ""},
		"the receiver's key":       {receiver{Receiver: Receiver{DedupeKey: byBody}}, HMAC{}, "evt_1"},
		"the key over a signed id": {receiver{Receiver: Receiver{DedupeKey: byBody}}, Arandu{}, "evt_1"},
		"an empty key over the id": {receiver{Receiver: Receiver{DedupeKey: func(*http.Request, []byte) string { return "" }}}, Arandu{}, ""},
	} {
		if got := deliveryKey(&c.rc, c.v, r, []byte(`{"id":"evt_1"}`)); got != c.want {
			t.Errorf("%s: the delivery key is %q, want %q", name, got, c.want)
		}
	}

	long := receiver{Receiver: Receiver{DedupeKey: func(*http.Request, []byte) string { return strings.Repeat("x", 200) }}}
	if got := deliveryKey(&long, HMAC{}, r, nil); !strings.HasPrefix(got, "sha256:") || len(got) > maxDeliveryIDLen {
		t.Errorf("a key wider than the column is kept as %q, want its digest", got)
	}
}

// TestACallIsKeptLongerThanItsSignatureChecks: a repeat is told apart by the
// stored call, so a receiver whose signature stays acceptable for longer than
// half the calls are kept is refused where it is wired.
func TestACallIsKeptLongerThanItsSignatureChecks(t *testing.T) {
	base := func(v SignatureValidator, days int, spec string) Config {
		return Config{Key: []byte(strings.Repeat("k", MinSecretBytes)), DeleteAfterDays: days, CleanSpec: spec, Receivers: []Receiver{{
			Name: "billing", Tenant: "acme", Secret: strings.Repeat("s", 32), Validator: v,
			Process: ProcessorFunc(nil),
		}}}
	}
	for name, c := range map[string]struct {
		cfg  Config
		fail bool
	}{
		"the default tolerance":                {base(Arandu{}, 0, ""), false},
		"a tolerance of a day, kept one":       {base(Arandu{Tolerance: 24 * time.Hour}, 1, ""), true},
		"a tolerance of a day, kept two":       {base(StandardWebhooks{Tolerance: 24 * time.Hour}, 2, ""), false},
		"a week's tolerance, kept the default": {base(StandardWebhooks{Tolerance: 7 * 24 * time.Hour}, 0, ""), false},
		"never cleaned":                        {base(Arandu{Tolerance: 30 * 24 * time.Hour}, 1, "-"), false},
		"a body-only HMAC":                     {base(HMAC{}, 1, ""), false},
	} {
		err := c.cfg.Validate()
		if (err != nil) != c.fail {
			t.Errorf("%s: Validate = %v, want refused %v", name, err, c.fail)
		}
	}
}
