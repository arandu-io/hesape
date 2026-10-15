package webhook_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/arandu-io/hesape/webhook"
)

// standardSecret, standardID and standardBody are the example of the Standard
// Webhooks specification, and standardSignature what it says they sign to.
const (
	standardSecret    = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
	standardID        = "msg_p5jXN8AQM9LWM0D4loKWxJek"
	standardTimestamp = 1614265330
	standardBody      = `{"test": 2432232314}`
	standardSignature = "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="
)

func TestSignStandardMatchesTheSpecificationsExample(t *testing.T) {
	t.Parallel()

	got, err := webhook.SignStandard([]string{standardSecret}, standardID, standardTimestamp, []byte(standardBody))
	if err != nil || got != standardSignature {
		t.Fatalf("SignStandard() = %q, %v, want %q", got, err, standardSignature)
	}
	both, err := webhook.SignStandard([]string{standardSecret, standardSecret}, standardID, standardTimestamp, []byte(standardBody))
	if err != nil || both != standardSignature+" "+standardSignature {
		t.Fatalf("two secrets sign to %q, %v", both, err)
	}
	for _, secret := range []string{"", "MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw", "whsec_not base64", "whsec_c2hvcnQ="} {
		if _, err := webhook.SignStandard([]string{secret}, standardID, standardTimestamp, nil); !errors.Is(err, webhook.ErrStandardSecret) {
			t.Errorf("SignStandard(%q) = %v, want ErrStandardSecret", secret, err)
		}
	}
}

func TestTheStandardWebhooksValidatorChecksTheSignatureAndItsAge(t *testing.T) {
	t.Parallel()

	signedAt := time.Unix(standardTimestamp, 0)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set(webhook.StandardHeaderID, standardID)
	req.Header.Set(webhook.StandardHeaderTimestamp, strconv.Itoa(standardTimestamp))
	req.Header.Set(webhook.StandardHeaderSignature, "v1,bm90IGl0 "+standardSignature)
	v := webhook.StandardWebhooks{Now: func() time.Time { return signedAt.Add(time.Minute) }}

	if !v.Valid(req, []byte(standardBody), webhook.SecretSet{Current: []byte("whsec_" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), Previous: [][]byte{[]byte(standardSecret)}}) {
		t.Fatal("a call signed with a secret still accepted during a rotation was refused")
	}
	if v.Valid(req, []byte(standardBody+" "), webhook.SecretSet{Current: []byte(standardSecret)}) {
		t.Fatal("a call whose body changed was accepted")
	}
	late := webhook.StandardWebhooks{Now: func() time.Time { return signedAt.Add(webhook.DefaultTolerance + time.Second) }}
	if late.Valid(req, []byte(standardBody), webhook.SecretSet{Current: []byte(standardSecret)}) {
		t.Fatal("a call older than the tolerance was accepted")
	}
}

func TestTheHMACValidatorChecksTheBodyUnderEveryAcceptedSecret(t *testing.T) {
	t.Parallel()

	body := []byte(`{"ok":true}`)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Hub-Signature-256", "sha256="+webhook.SignBody([]byte(testKey), body))
	v := webhook.HMAC{Header: "X-Hub-Signature-256", Prefix: "sha256="}
	if !v.Valid(req, body, webhook.SecretSet{Current: []byte("another-secret-another-secret-01"), Previous: [][]byte{[]byte(testKey)}}) {
		t.Fatal("a body signed with a previous secret was refused")
	}
	if v.Valid(req, []byte(`{"ok":false}`), webhook.SecretSet{Current: []byte(testKey)}) {
		t.Fatal("another body was accepted")
	}
	if (webhook.HMAC{Header: "X-Hub-Signature-256"}).Valid(req, body, webhook.SecretSet{Current: []byte(testKey)}) {
		t.Fatal("a signature with an unexpected prefix was accepted")
	}
}

func TestTheAranduValidatorRefusesAReplay(t *testing.T) {
	t.Parallel()

	body := []byte(`{"ok":true}`)
	signedAt := time.Unix(1700000000, 0)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Acme-Timestamp", "1700000000")
	req.Header.Set("X-Acme-Delivery-Id", "delivery-1")
	req.Header.Set("X-Acme-Signature", webhook.Sign([]byte(testKey), "1700000000", "delivery-1", body))
	secrets := webhook.SecretSet{Current: []byte(testKey)}

	if !(webhook.Arandu{HeaderPrefix: "X-Acme", Now: func() time.Time { return signedAt }}).Valid(req, body, secrets) {
		t.Fatal("a fresh call was refused")
	}
	if (webhook.Arandu{HeaderPrefix: "X-Acme", Now: func() time.Time { return signedAt.Add(10 * time.Minute) }}).Valid(req, body, secrets) {
		t.Fatal("a call ten minutes old was accepted")
	}
	if (webhook.Arandu{Now: func() time.Time { return signedAt }}).Valid(req, body, secrets) {
		t.Fatal("the validator read headers under a prefix it was not given")
	}
}

func TestTheHeaderSecretValidatorComparesTheHeaderAlone(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Notification", "shared")
	v := webhook.HeaderSecret{Header: "X-Notification"}
	if !v.Valid(req, nil, webhook.SecretSet{Previous: [][]byte{[]byte("shared")}}) {
		t.Fatal("a secret still accepted during a rotation was refused")
	}
	if v.Valid(req, nil, webhook.SecretSet{}) {
		t.Fatal("a receiver with no secret accepted a call")
	}
	if (webhook.HeaderSecret{}).Valid(req, nil, webhook.SecretSet{Current: []byte("shared")}) {
		t.Fatal("a validator naming no header accepted a call")
	}
}
