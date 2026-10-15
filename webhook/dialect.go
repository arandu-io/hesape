package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// aranduPrefix is the prefix the Arandu dialect's headers carry unless
// Server.HeaderPrefix renames them: X-Arandu-Signature, X-Arandu-Timestamp
// and X-Arandu-Delivery-Id.
const aranduPrefix = "X-Arandu"

// StandardSecretPrefix labels a Standard Webhooks secret: what follows it is
// the standard base64 of the signing key.
const StandardSecretPrefix = "whsec_"

// The Standard Webhooks headers, as Go writes them on the wire.
const (
	StandardHeaderID        = "Webhook-Id"
	StandardHeaderTimestamp = "Webhook-Timestamp"
	StandardHeaderSignature = "Webhook-Signature"
)

// The length a Standard Webhooks key may decode to.
const (
	minStandardKeyBytes = 24
	maxStandardKeyBytes = 64
)

// ErrStandardSecret is a secret a Standard Webhooks endpoint cannot sign with:
// it is not whsec_ and the standard base64 of a key of 24 to 64 bytes.
var ErrStandardSecret = errors.New("webhook: a Standard Webhooks secret is whsec_ and the standard base64 of a 24 to 64 byte key")

// standardKey is the key a whsec_ secret encodes.
func standardKey(secret string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(secret, StandardSecretPrefix)
	if !ok {
		return nil, ErrStandardSecret
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) < minStandardKeyBytes || len(key) > maxStandardKeyBytes {
		return nil, ErrStandardSecret
	}
	return key, nil
}

// SignStandard returns the value of a Webhook-Signature header: "v1," and
// the standard base64 of the HMAC-SHA256 of id.timestamp.body under each
// secret's key, in the order given, separated by a space.
func SignStandard(secrets []string, id string, timestamp int64, body []byte) (string, error) {
	if len(secrets) == 0 {
		return "", ErrStandardSecret
	}
	prefix := id + "." + strconv.FormatInt(timestamp, 10) + "."
	parts := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		key, err := standardKey(secret)
		if err != nil {
			return "", err
		}
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte(prefix))
		_, _ = mac.Write(body)
		parts = append(parts, "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	}
	return strings.Join(parts, " "), nil
}

// signing is what one attempt signs with: the secrets still signing, newest
// first, the first being the current one.
type signing struct {
	secrets []string
}

func (s signing) current() []byte {
	if len(s.secrets) == 0 {
		return nil
	}
	return []byte(s.secrets[0])
}

// oldestFirst is the secrets still signing, from the one that retires first
// to the current one: outside a rotation, the current one alone. A previous
// secret too short to sign with is left out; the current one is checked by
// the caller.
func (s signing) oldestFirst() [][]byte {
	out := make([][]byte, 0, len(s.secrets))
	for i := len(s.secrets) - 1; i > 0; i-- {
		if len(s.secrets[i]) >= MinSecretBytes {
			out = append(out, []byte(s.secrets[i]))
		}
	}
	return append(out, s.current())
}

// sign writes the dialect's headers on the request: the content type, the
// user agent when the policy has one, and the signature of the body at the
// timestamp. Extra headers were set before it and cannot override these.
//
// During a rotation each dialect signs so that a receiver holding a secret
// still signing accepts the call. Standard Webhooks lists one signature per
// secret in its header, as the specification says. The Arandu signature
// header is written once per secret, on its own line, the secret that
// retires first on the first line: a receiver that reads one value keeps the
// secret it has until that secret retires, and Verify accepts any of them.
// The HMAC dialect's header is one hex value that receivers outside this
// package read whole, so it is signed with the secret that retires first
// alone, and moves to the next secret when that one retires. Outside a
// rotation every header carries the one signature it always carried.
func sign(h http.Header, s shot, signedAt time.Time, prefix, userAgent string) error {
	body, keys := s.body, s.keys
	h.Set("Content-Type", "application/json")
	if userAgent != "" {
		h.Set("User-Agent", userAgent)
	}
	switch s.dialect {
	case DialectStandardWebhooks:
		signature, err := SignStandard(keys.secrets, s.id, signedAt.Unix(), body)
		if err != nil {
			return err
		}
		h.Set(StandardHeaderID, s.id)
		h.Set(StandardHeaderTimestamp, strconv.FormatInt(signedAt.Unix(), 10))
		h.Set(StandardHeaderSignature, signature)
		return nil
	case DialectHMAC:
		if len(keys.current()) < MinSecretBytes {
			return ErrSecretTooShort
		}
		h.Set(s.signatureHeader, SignBody(keys.oldestFirst()[0], body))
		if !s.aranduHeaders {
			return nil
		}
	}
	if len(keys.current()) < MinSecretBytes {
		return ErrSecretTooShort
	}
	timestamp := strconv.FormatInt(signedAt.Unix(), 10)
	h.Set(prefix+"-Delivery-Id", s.id)
	h.Set(prefix+"-Timestamp", timestamp)
	h.Del(prefix + "-Signature")
	for _, secret := range keys.oldestFirst() {
		h.Add(prefix+"-Signature", Sign(secret, timestamp, s.id, body))
	}
	return nil
}
