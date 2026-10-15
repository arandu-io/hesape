package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/arandu-io/hesape/encryption"
)

const signaturePrefix = "sha256="

// NormalizeURL validates and returns an absolute HTTP or HTTPS endpoint URL.
func NormalizeURL(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || len(normalized) > MaxURLLength {
		return "", ErrInvalidURL
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed == nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil {
		return "", ErrInvalidURL
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrInvalidURL
	}
	return parsed.String(), nil
}

// Sign computes the wire signature over timestamp, delivery id and body.
func Sign(secret []byte, timestamp, deliveryID string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write([]byte(deliveryID))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// maxSignatures is how many signatures one Arandu signature header may list
// before it is refused: one per secret still signing, and a sender keeps at
// most MaxPreviousSecrets beside the current one.
const maxSignatures = MaxPreviousSecrets + 1

// Verify compares a signature against the active and rotation secrets in
// constant time. signature is one sha256= value, or several separated by
// commas -- what a sender writes during its own rotation, one per secret it
// still signs with, as separate header lines a reader may join -- and one of
// them matching one secret is enough. A malformed value, or more values than
// a sender signs with, is refused.
func Verify(secrets SecretSet, timestamp, deliveryID string, body []byte, signature string) bool {
	if strings.Count(signature, ",") >= maxSignatures {
		return false
	}
	var provided [][]byte
	for _, entry := range strings.Split(signature, ",") {
		entry = strings.TrimSpace(entry)
		decoded, err := hex.DecodeString(strings.TrimPrefix(entry, signaturePrefix))
		if err != nil || !strings.HasPrefix(entry, signaturePrefix) || len(decoded) != sha256.Size {
			return false
		}
		provided = append(provided, decoded)
	}
	all := make([][]byte, 0, len(secrets.Previous)+1)
	all = append(all, secrets.Current)
	all = append(all, secrets.Previous...)
	matched := 0
	for _, secret := range all {
		if len(secret) == 0 {
			continue
		}
		expected, _ := hex.DecodeString(strings.TrimPrefix(Sign(secret, timestamp, deliveryID, body), signaturePrefix))
		for _, given := range provided {
			if hmac.Equal(given, expected) {
				matched = 1
			}
		}
	}
	return matched == 1
}

// SignatureValidator decides whether a received call was signed by the
// sender that shares the receiver's secret. secrets holds the current secret and the ones still
// accepted during a rotation; body is exactly what arrived.
type SignatureValidator interface {
	Valid(r *http.Request, body []byte, secrets SecretSet) bool
}

// SignatureValidatorFunc adapts a function to SignatureValidator.
type SignatureValidatorFunc func(r *http.Request, body []byte, secrets SecretSet) bool

// Valid calls f.
func (f SignatureValidatorFunc) Valid(r *http.Request, body []byte, secrets SecretSet) bool {
	return f(r, body, secrets)
}

// HMAC is the validator a receiver has by default: the header carries the
// hex HMAC-SHA256 of the body under the shared secret. Prefix is what comes
// before the hex, such as "sha256=" for a sender that writes one; empty means
// none.
type HMAC struct {
	Header string
	Prefix string
}

// Valid compares the header with the signature of the body under every
// accepted secret, in constant time.
func (v HMAC) Valid(r *http.Request, body []byte, secrets SecretSet) bool {
	header := v.Header
	if header == "" {
		header = DefaultSignatureHeader
	}
	given := strings.TrimSpace(r.Header.Get(header))
	if v.Prefix != "" {
		if !strings.HasPrefix(given, v.Prefix) {
			return false
		}
		given = strings.TrimPrefix(given, v.Prefix)
	}
	provided, err := hex.DecodeString(given)
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	matched := false
	for _, secret := range allSecrets(secrets) {
		if hmac.Equal(provided, bodyMAC(secret, body)) {
			matched = true
		}
	}
	return matched
}

// DefaultTolerance is how far a signed timestamp may be from now before an
// Arandu signature is refused as a replay.
const DefaultTolerance = 5 * time.Minute

// Arandu checks the scheme Sign writes: the Signature, Timestamp and
// Delivery-ID headers under HeaderPrefix, sha256= over the timestamp, the
// delivery id and the body. A timestamp further than Tolerance from now is
// refused, so a captured call cannot be sent again later.
type Arandu struct {
	// HeaderPrefix is the senders' header prefix. Empty means X-Arandu.
	HeaderPrefix string
	// Tolerance is the accepted clock difference. Zero means
	// DefaultTolerance.
	Tolerance time.Duration
	// Now is the clock, replaceable by a test. Nil is time.Now.
	Now func() time.Time
}

// Valid checks the timestamp's age, then the signature: every line of the
// signature header, any of which may match.
func (v Arandu) Valid(r *http.Request, body []byte, secrets SecretSet) bool {
	prefix := v.HeaderPrefix
	if prefix == "" {
		prefix = aranduPrefix
	}
	tolerance := v.Tolerance
	if tolerance == 0 {
		tolerance = DefaultTolerance
	}
	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}
	timestamp := r.Header.Get(prefix + "-Timestamp")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if age := now.Sub(time.Unix(seconds, 0)); age > tolerance || age < -tolerance {
		return false
	}
	signature := strings.Join(r.Header.Values(prefix+"-Signature"), ",")
	return Verify(secrets, timestamp, r.Header.Get(prefix+"-Delivery-ID"), body, signature)
}

// SignBody returns the signature of a body DialectHMAC sends: the hex
// HMAC-SHA256 under the secret.
func SignBody(secret []byte, body []byte) string {
	return hex.EncodeToString(bodyMAC(secret, body))
}

func bodyMAC(secret, body []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return mac.Sum(nil)
}

func allSecrets(set SecretSet) [][]byte {
	out := make([][]byte, 0, len(set.Previous)+1)
	for _, secret := range append([][]byte{set.Current}, set.Previous...) {
		if len(secret) > 0 {
			out = append(out, secret)
		}
	}
	return out
}

// sealer seals an endpoint's secret and headers with a key derived from the
// application's key for this purpose alone. The sealed text carries the
// tenant, the endpoint and the purpose it was sealed for, so a value copied
// onto another row does not open there.
type sealer struct{ enc *encryption.Encrypter }

func newSealer(appKey []byte) (*sealer, error) {
	mac := hmac.New(sha256.New, appKey)
	// The label is fixed: every secret and header already sealed at rest was
	// sealed under the key it derives.
	_, _ = mac.Write([]byte("arandu-webhook:endpoint:seal:v1"))
	enc, err := encryption.NewEncrypter(mac.Sum(nil), encryption.AES256GCM)
	if err != nil {
		return nil, err
	}
	return &sealer{enc: enc}, nil
}

func sealPrefix(tenant, id, purpose string) string {
	return "webhook:v1\x00" + tenant + "\x00" + id + "\x00" + purpose + "\x00"
}

func (s *sealer) seal(tenant, id, purpose, value string) (string, error) {
	return s.enc.EncryptString(sealPrefix(tenant, id, purpose) + value)
}

// errSealedElsewhere is a sealed value opened on a row it was not sealed for.
var errSealedElsewhere = errors.New("webhook: the sealed value belongs to another endpoint")

func (s *sealer) open(tenant, id, purpose, sealed string) (string, error) {
	plain, err := s.enc.DecryptString(sealed)
	if err != nil {
		return "", err
	}
	prefix := sealPrefix(tenant, id, purpose)
	if !strings.HasPrefix(plain, prefix) {
		return "", errSealedElsewhere
	}
	return strings.TrimPrefix(plain, prefix), nil
}

// PreviousSecret is a secret that still signs after a rotation, until it
// retires.
type PreviousSecret struct {
	Secret    string    `json:"secret"`
	RetiresAt time.Time `json:"retires_at"`
}

// sealPrevious seals an endpoint's previous secrets; none seals to empty.
func (s *sealer) sealPrevious(tenant, id string, previous []PreviousSecret) (string, error) {
	if len(previous) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(previous)
	if err != nil {
		return "", err
	}
	return s.seal(tenant, id, "previous-secrets", string(encoded))
}

// openPrevious opens an endpoint's previous secrets; empty opens to none.
func (s *sealer) openPrevious(tenant, id, sealed string) ([]PreviousSecret, error) {
	if sealed == "" {
		return nil, nil
	}
	plain, err := s.open(tenant, id, "previous-secrets", sealed)
	if err != nil {
		return nil, err
	}
	var out []PreviousSecret
	if err := json.Unmarshal([]byte(plain), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// headerValidator is a validator that reads only the request's headers, so
// the receiver asks it before reading the body: a call it refuses is refused
// whatever its body is.
type headerValidator interface {
	readsHeadersOnly() bool
}

// HeaderSecret checks a shared secret sent as it is in a header, as a
// provider that signs nothing sends it: the header has to equal the current
// secret or one still accepted during a rotation, compared in constant time.
// A receiver with no secret refuses every call. The body is not read before
// the check.
type HeaderSecret struct {
	// Header carries the secret.
	Header string
}

// Valid compares the header with every accepted secret.
func (v HeaderSecret) Valid(r *http.Request, _ []byte, secrets SecretSet) bool {
	given := r.Header.Get(v.Header)
	if v.Header == "" || given == "" {
		return false
	}
	matched := 0
	for _, secret := range allSecrets(secrets) {
		matched |= subtle.ConstantTimeCompare([]byte(given), secret)
	}
	return matched == 1
}

func (HeaderSecret) readsHeadersOnly() bool { return true }

// StandardWebhooks checks the Standard Webhooks scheme: webhook-signature
// carries "v1," and the standard base64 HMAC-SHA256 of id.timestamp.body
// under the key a whsec_ secret encodes, once per secret the sender signs
// with; one matching any accepted secret is enough. A timestamp further than
// Tolerance from now is refused, so a captured call cannot be sent again
// later.
type StandardWebhooks struct {
	// Tolerance is the accepted clock difference. Zero means
	// DefaultTolerance.
	Tolerance time.Duration
	// Now is the clock, replaceable by a test. Nil is time.Now.
	Now func() time.Time
}

// Valid checks the timestamp's age, then every signature against every
// accepted secret.
func (v StandardWebhooks) Valid(r *http.Request, body []byte, secrets SecretSet) bool {
	tolerance := v.Tolerance
	if tolerance == 0 {
		tolerance = DefaultTolerance
	}
	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}
	id, timestamp := r.Header.Get(StandardHeaderID), r.Header.Get(StandardHeaderTimestamp)
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if id == "" || err != nil {
		return false
	}
	if age := now.Sub(time.Unix(seconds, 0)); age > tolerance || age < -tolerance {
		return false
	}
	var given [][]byte
	for _, entry := range strings.Fields(r.Header.Get(StandardHeaderSignature)) {
		version, encoded, ok := strings.Cut(entry, ",")
		if !ok || version != "v1" {
			continue
		}
		if signature, err := base64.StdEncoding.DecodeString(encoded); err == nil {
			given = append(given, signature)
		}
	}
	matched := false
	for _, secret := range allSecrets(secrets) {
		key, err := standardKey(string(secret))
		if err != nil {
			continue
		}
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte(id + "." + timestamp + "."))
		_, _ = mac.Write(body)
		expected := mac.Sum(nil)
		for _, signature := range given {
			if hmac.Equal(signature, expected) {
				matched = true
			}
		}
	}
	return matched
}
