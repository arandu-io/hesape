package providers

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arandu-io/hesape/oauth"
)

const (
	// maxIDTokenBytes bounds the token before any of it is decoded. Apple's
	// and Google's are about 1 KiB; the bound exists so that a header that
	// carries a megabyte is refused by length and not by running out of memory
	// parsing it.
	maxIDTokenBytes = 16 << 10
	// idTokenLeeway is the clock difference between this server and the
	// provider's that is forgiven on exp, nbf and iat.
	idTokenLeeway = time.Minute
)

// ErrInvalidIDToken is wrapped by every error [VerifyIDToken] returns because
// of the token itself: malformed, signed by nobody the provider published,
// signed with another algorithm, expired, issued for another application, or
// carrying a nonce that is not the one expected.
//
// The reason is in the message and the token never is. A caller answers every
// one the same way, with a refusal, and logs the error as it is.
var ErrInvalidIDToken = errors.New("oauth: the ID token is not valid")

// ErrExpiredIDToken is the [ErrInvalidIDToken] whose only fault is its age. It
// is its own value because a native app that holds a token a few minutes past
// its time can be told to ask the platform for a new one; every other fault is
// not fixed by asking again.
var ErrExpiredIDToken = fmt.Errorf("%w: it has expired", ErrInvalidIDToken)

// TokenIssuer is who signs ID tokens: the values of the iss claim it uses and
// the [KeySet] its public keys are published in.
type TokenIssuer struct {
	issuers []string
	keys    *KeySet
}

// NewTokenIssuer builds an issuer for a provider this package does not carry.
// A token is accepted when its iss is one of issuers, and it must be signed
// with RS256 by a key in keys.
func NewTokenIssuer(keys *KeySet, issuers ...string) TokenIssuer {
	return TokenIssuer{issuers: append([]string(nil), issuers...), keys: keys}
}

// GoogleIssuer is Google, as the ID tokens of Sign in with Google carry it:
// iss is "https://accounts.google.com" (the older "accounts.google.com" is
// accepted too, as Google documents), signed by the keys of [GoogleKeys].
func GoogleIssuer() TokenIssuer {
	return NewTokenIssuer(GoogleKeys(), "https://accounts.google.com", "accounts.google.com")
}

// AppleIssuer is Apple: iss is "https://appleid.apple.com", signed by the keys
// of [AppleKeys].
func AppleIssuer() TokenIssuer {
	return NewTokenIssuer(AppleKeys(), "https://appleid.apple.com")
}

// WithKeys is the same issuer reading its keys from another set, such as one
// built with [NewKeySet] for a different address or a different client.
func (i TokenIssuer) WithKeys(keys *KeySet) TokenIssuer {
	i.keys = keys
	return i
}

// IDTokenOption changes what [VerifyIDToken] checks.
type IDTokenOption func(*idTokenCheck)

type idTokenCheck struct {
	nonce    string
	nonceSet bool
}

// WithNonce requires the token to carry exactly this nonce. A token without
// one is refused.
//
// A native app generates the nonce, hands the platform a hash of it, and sends
// the server the token and the nonce it started from; the server compares. It
// is what ties a token to the sign-in that asked for it, so a token taken from
// one sign-in cannot be presented in another.
func WithNonce(nonce string) IDTokenOption {
	return func(c *idTokenCheck) {
		c.nonce = nonce
		c.nonceSet = true
	}
}

// IDTokenClaims is what a verified ID token says. Nothing in it is trusted
// before [VerifyIDToken] returns it, and everything in it is the provider's.
type IDTokenClaims struct {
	// Issuer is the iss claim.
	Issuer string
	// Subject is the sub claim: the person's identifier at the provider, which
	// is stable and is the only thing to link an account by. An e-mail address
	// is not.
	Subject string
	// Audience is the aud claim: the applications the token was issued for.
	Audience []string
	// AuthorizedParty is Google's azp claim: the application that asked.
	AuthorizedParty string
	// ExpiresAt and IssuedAt are exp and iat. IssuedAt is the zero time when
	// the token did not carry one.
	ExpiresAt time.Time
	IssuedAt  time.Time
	// Nonce is the nonce claim, empty when there is none.
	Nonce string
	// Email is the e-mail claim, empty when the person did not share one.
	Email string
	// EmailVerified is whether the provider vouches that the person controls
	// Email. The claim arrives as a boolean or as the text "true"; both are
	// read, and a missing or any other value is false.
	EmailVerified bool
	// IsPrivateEmail is Apple's flag for an address that forwards through
	// Apple's relay: it reaches the person and says nothing about who they are.
	IsPrivateEmail bool
	// Name and Picture are Google's claims of the same names.
	Name    string
	Picture string

	raw map[string]any
}

// All is every claim of the token, as decoded: numbers arrive as
// [json.Number].
func (c IDTokenClaims) All() map[string]any {
	out := make(map[string]any, len(c.raw))
	for k, v := range c.raw {
		out[k] = v
	}
	return out
}

// UserData is the person the token describes, in the shape every provider of
// this package answers with: id and sub (the subject), and email,
// email_verified, is_private_email, name, picture, given_name and family_name
// for each of them the token carried. email_verified and is_private_email are
// booleans, whatever the provider sent, so [oauth.UserData.Bool] and a type
// assertion agree.
func (c IDTokenClaims) UserData() oauth.UserData {
	params := map[string]any{
		"id":  c.Subject,
		"sub": c.Subject,
	}
	if c.Email != "" {
		params["email"] = c.Email
		params["email_verified"] = c.EmailVerified
	}
	if _, ok := c.raw["is_private_email"]; ok {
		params["is_private_email"] = c.IsPrivateEmail
	}
	for _, key := range []string{"name", "picture", "given_name", "family_name"} {
		if v, ok := c.raw[key].(string); ok && v != "" {
			params[key] = v
		}
	}
	return oauth.NewUserData(params)
}

// VerifyIDToken checks an OpenID Connect ID token and returns what it says.
//
// It is for the token a native app hands the server -- Sign in with Apple on
// iOS, Google Sign-In on Android -- where the platform returns the token to
// the app directly and no redirect, no state and no code exchange takes
// place. The web flow of [NewAppleProvider] runs the same check on the token it
// receives.
//
//	claims, err := providers.VerifyIDToken(ctx, body.IDToken,
//		providers.AppleIssuer(), []string{"com.example.app"},
//		providers.WithNonce(body.Nonce))
//	if errors.Is(err, providers.ErrInvalidIDToken) {
//		// 401: the token is not what it says it is
//	}
//
// The token is accepted only when all of this holds:
//
//   - it is three base64url segments, within a size limit;
//   - the header names RS256. "none", the HMAC algorithms -- which would let a
//     public key be used as a shared secret -- and anything else are refused,
//     as is a header with a crit list this package would have to understand;
//   - the header names a key id the issuer publishes, and the signature
//     verifies with that key. The claims are not read before it does;
//   - iss is one of the issuer's, and aud holds at least one of audiences. An
//     empty audiences is a mistake and is refused rather than read as "any":
//     a token issued for another application is a valid token, and the
//     audience is the only thing that says it was not meant for this one;
//   - exp has not passed, and nbf and iat are not in the future, allowing a
//     minute of clock difference;
//   - sub is present, and when [WithNonce] is given the nonce claim is equal.
//
// Strings are compared in constant time. The keys come from the issuer's
// [KeySet], cached and renewed as it describes; when they cannot be fetched
// the error wraps [ErrKeysUnavailable] and not [ErrInvalidIDToken]. Nothing
// the token carries is repeated in an error.
func VerifyIDToken(ctx context.Context, idToken string, issuer TokenIssuer, audiences []string, options ...IDTokenOption) (IDTokenClaims, error) {
	var check idTokenCheck
	for _, option := range options {
		option(&check)
	}
	switch {
	case len(audiences) == 0:
		return IDTokenClaims{}, errors.New("oauth: VerifyIDToken was given no audience, and a token issued for another application would pass (pass the client ids this server accepts)")
	case issuer.keys == nil || len(issuer.issuers) == 0:
		return IDTokenClaims{}, errors.New("oauth: VerifyIDToken was given an issuer with no keys or no iss value (build it with GoogleIssuer, AppleIssuer or NewTokenIssuer)")
	case check.nonceSet && check.nonce == "":
		return IDTokenClaims{}, errors.New("oauth: WithNonce was given an empty nonce, which would skip the check it asks for")
	}

	header, payload, signature, signed, err := splitIDToken(idToken)
	if err != nil {
		return IDTokenClaims{}, err
	}
	key, err := issuer.keys.lookup(ctx, header.Kid)
	if err != nil {
		return IDTokenClaims{}, err
	}
	if key == nil {
		return IDTokenClaims{}, invalid("it is signed with a key the issuer does not publish")
	}
	digest := sha256.Sum256(signed)
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return IDTokenClaims{}, invalid("the signature does not verify")
	}

	// From here the claims are the issuer's own words.
	raw, err := decodeClaims(payload)
	if err != nil {
		return IDTokenClaims{}, err
	}
	return checkClaims(raw, issuer, audiences, check, time.Now())
}

// invalid is an [ErrInvalidIDToken] with its reason. The reason is a fixed
// sentence: it never carries a value taken from the token.
func invalid(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidIDToken, reason)
}

// idTokenHeader is the part of the JOSE header this package reads.
type idTokenHeader struct {
	Alg  string          `json:"alg"`
	Kid  string          `json:"kid"`
	Crit json.RawMessage `json:"crit"`
}

// splitIDToken takes the token apart and checks its shape, up to and not
// including the signature.
func splitIDToken(token string) (header idTokenHeader, payload, signature, signed []byte, err error) {
	if token == "" || len(token) > maxIDTokenBytes {
		return header, nil, nil, nil, invalid("it is empty or larger than the limit")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return header, nil, nil, nil, invalid("it is not three segments")
	}
	decoded := make([][]byte, 3)
	for i, part := range parts {
		b, ok := decodeSegment(part)
		if !ok {
			return header, nil, nil, nil, invalid("a segment is not base64url")
		}
		decoded[i] = b
	}
	if err := json.Unmarshal(decoded[0], &header); err != nil {
		return header, nil, nil, nil, invalid("the header is not JSON")
	}
	// The algorithm is fixed by this package and not read from the token. The
	// comparison is the whole defence against "none" and against HS256 with
	// the issuer's public key as the secret.
	if subtle.ConstantTimeCompare([]byte(header.Alg), []byte("RS256")) != 1 {
		return header, nil, nil, nil, invalid("the algorithm is not RS256")
	}
	if len(header.Crit) > 0 && !bytes.Equal(bytes.TrimSpace(header.Crit), []byte("null")) {
		return header, nil, nil, nil, invalid("the header requires an extension this package does not know")
	}
	if header.Kid == "" {
		return header, nil, nil, nil, invalid("the header names no key")
	}
	if len(decoded[2]) == 0 {
		return header, nil, nil, nil, invalid("it has no signature")
	}
	return header, decoded[1], decoded[2], []byte(parts[0] + "." + parts[1]), nil
}

// decodeSegment reads one base64url segment without padding. The alphabet is
// checked first because the decoder skips line breaks, and two spellings of one
// token are two tokens to anything that compares or caches them.
func decodeSegment(s string) ([]byte, bool) {
	if s == "" {
		return nil, false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return nil, false
		}
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil {
		return nil, false
	}
	return b, true
}

// decodeClaims reads the payload into a map, with numbers kept as
// [json.Number] so an exp is not rounded through a float.
func decodeClaims(payload []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil || raw == nil {
		return nil, invalid("the claims are not a JSON object")
	}
	return raw, nil
}

// checkClaims applies the claim rules to a payload whose signature verified.
func checkClaims(raw map[string]any, issuer TokenIssuer, audiences []string, check idTokenCheck, now time.Time) (IDTokenClaims, error) {
	var claims IDTokenClaims
	claims.raw = raw

	claims.Issuer = claimString(raw, "iss")
	if !equalToAny(claims.Issuer, issuer.issuers) {
		return IDTokenClaims{}, invalid("it was not issued by the expected issuer")
	}

	claims.Audience = claimAudience(raw)
	allowed := false
	for _, aud := range claims.Audience {
		if equalToAny(aud, audiences) {
			allowed = true
		}
	}
	if !allowed {
		return IDTokenClaims{}, invalid("it was issued for another application")
	}

	exp, ok := claimTime(raw, "exp")
	if !ok {
		return IDTokenClaims{}, invalid("it has no expiry")
	}
	if now.After(exp.Add(idTokenLeeway)) {
		return IDTokenClaims{}, ErrExpiredIDToken
	}
	claims.ExpiresAt = exp
	if nbf, ok := claimTime(raw, "nbf"); ok && now.Add(idTokenLeeway).Before(nbf) {
		return IDTokenClaims{}, invalid("it is not valid yet")
	}
	if iat, ok := claimTime(raw, "iat"); ok {
		if now.Add(idTokenLeeway).Before(iat) {
			return IDTokenClaims{}, invalid("it was issued in the future")
		}
		claims.IssuedAt = iat
	}

	claims.Subject = claimString(raw, "sub")
	if claims.Subject == "" {
		return IDTokenClaims{}, invalid("it has no subject")
	}
	claims.Nonce = claimString(raw, "nonce")
	if check.nonceSet && (claims.Nonce == "" || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(check.nonce)) != 1) {
		return IDTokenClaims{}, invalid("the nonce is not the one expected")
	}

	claims.AuthorizedParty = claimString(raw, "azp")
	claims.Email = claimString(raw, "email")
	claims.EmailVerified = claimBool(raw, "email_verified")
	claims.IsPrivateEmail = claimBool(raw, "is_private_email")
	claims.Name = claimString(raw, "name")
	claims.Picture = claimString(raw, "picture")
	return claims, nil
}

// equalToAny compares in constant time against every candidate, without
// stopping at the first match.
func equalToAny(value string, candidates []string) bool {
	found := 0
	for _, candidate := range candidates {
		found |= subtle.ConstantTimeCompare([]byte(value), []byte(candidate))
	}
	return found == 1
}

func claimString(raw map[string]any, key string) string {
	s, _ := raw[key].(string)
	return s
}

// claimBool reads a flag that is a boolean or the text "true". Anything else,
// including the text "false", a number and an absent claim, is false.
func claimBool(raw map[string]any, key string) bool {
	switch v := raw[key].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return false
}

// claimAudience reads aud, which is one string or a list of them.
func claimAudience(raw map[string]any) []string {
	switch v := raw["aud"].(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// claimTime reads a NumericDate: seconds since the epoch, whole or fractional.
func claimTime(raw map[string]any, key string) (time.Time, bool) {
	n, ok := raw[key].(json.Number)
	if !ok {
		return time.Time{}, false
	}
	seconds, err := n.Float64()
	// Bounded so that the conversion to nanoseconds cannot overflow: year 3000
	// is a long time past any token anybody issues.
	if err != nil || seconds < 0 || seconds > 32503680000 {
		return time.Time{}, false
	}
	whole := int64(seconds)
	return time.Unix(whole, int64((seconds-float64(whole))*1e9)), true
}
