package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
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

// Verify compares a signature against the active and rotation secrets in
// constant time. Malformed signatures are refused.
func Verify(secrets SecretSet, timestamp, deliveryID string, body []byte, signature string) bool {
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, signaturePrefix))
	if err != nil || !strings.HasPrefix(signature, signaturePrefix) || len(provided) != sha256.Size {
		return false
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
		if hmac.Equal(provided, expected) {
			matched = 1
		}
	}
	return matched == 1
}
