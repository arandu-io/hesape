package providers

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/oauth"
)

const (
	appleAudience = "https://appleid.apple.com"
	// appleSecretLifetime is how long the client secret is valid. It is made
	// for one exchange and thrown away, so five minutes is for the clock
	// difference and a slow network; Apple accepts up to six months, and a
	// secret that lives that long is a credential worth stealing.
	appleSecretLifetime = 5 * time.Minute
	// appleUserMaxBytes bounds the user field of the callback, which a browser
	// posts and a person can edit.
	appleUserMaxBytes = 4 << 10
)

// NewAppleProvider builds the Sign in with Apple provider.
//
// servicesID is the Services ID registered for the website, which is the
// client id; teamID and keyID are the Apple developer team and the key created
// for Sign in with Apple; privateKey is that key, the .p8 file, in PEM. A "\n"
// written as two characters, which is how an environment variable carries a
// PEM, is read as a line break.
//
//	store := providers.NewCookieStateStore(w, r)
//	provider := providers.NewAppleProvider(store, servicesID, teamID, keyID, p8).
//		RedirectURL("https://example.com/auth/apple/callback")
//	provider.Redirect(w, r) // GET /auth/apple
//
//	user, _, err := provider.User(r) // POST /auth/apple/callback
//
// This is a login, not a registration: what it answers is who the person is
// at Apple, and what to do with someone who has no account is the application's.
// The answer carries what the application needs to decide. [oauth.UserData]
// holds id (the subject, stable, and the one thing to link an account by),
// email, email_verified and is_private_email, the last two as booleans however
// Apple spelled them. An address that is not verified, or that is Apple's relay
// (is_private_email), proves nothing about which of the application's accounts
// the person owns, and linking by it is a decision to make on purpose.
//
// What differs from the other providers, and is handled here:
//
//   - The client secret is not a string but a JWT signed with the key, with
//     ES256, made for each exchange and valid for five minutes. It is never
//     written to a log or an error.
//   - The redirect asks for response_mode=form_post, which Apple requires when
//     the scope holds name or email. The callback is therefore a POST from
//     another site: [Provider.GetAccessToken] reads state and code from its
//     body, the route must accept POST, and it must be exempt from the
//     application's CSRF check (the state check takes its place). If the state
//     store is a [CookieStateStore], [CookieStateStore.CrossSite] is set so the
//     cookie travels on that POST; any other store must survive a cross-site
//     POST by itself.
//   - The person is in the id_token the exchange returns, verified with Apple's
//     published keys, issuer, audience, expiry and a nonce derived from the
//     state. See [VerifyIDToken].
//   - Apple sends the person's name once, in a user field of the first
//     callback and never again. [Provider.User] adds it to the answer as name,
//     given_name and family_name when it is there. The field is typed by the
//     browser and is not signed: the e-mail in it is ignored, and only the
//     claim in the id_token is an e-mail Apple vouches for.
//
// Mistakes in the credentials are reported by the first call that needs them
// ([Provider.Redirect], [Provider.GetAccessToken]) and not by the constructor,
// so that it chains like the others.
func NewAppleProvider(state StateStoreInterface, servicesID, teamID, keyID, privateKey string) *Provider {
	p := NewProvider(state, servicesID, "",
		"https://appleid.apple.com/auth/authorize",
		"https://appleid.apple.com/auth/token",
		"",
	)
	p.scopeDelimiter = " "
	p.defaultScope = []string{"name", "email"}
	p.authParameters = map[string]string{"response_mode": "form_post"}
	p.UserFromIDToken(AppleIssuer())
	p.callbackUser = appleCallbackUser

	if store, ok := state.(*CookieStateStore); ok && store != nil {
		store.CrossSite()
	}

	switch {
	case servicesID == "" || teamID == "" || keyID == "":
		p.configErr = errors.New("oauth: Sign in with Apple needs the Services ID, the Team ID and the Key ID")
	default:
		key, err := parseApplePrivateKey(privateKey)
		if err != nil {
			p.configErr = err
			break
		}
		p.secretFunc = func() (string, error) {
			return appleClientSecret(key, teamID, servicesID, keyID, time.Now())
		}
	}
	return p
}

// parseApplePrivateKey reads the .p8 file: a PKCS #8 EC private key on P-256.
// The error says what is wrong with the key and never repeats any of it.
func parseApplePrivateKey(text string) (*ecdsa.PrivateKey, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, `\n`, "\n"))
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, errors.New("oauth: the Sign in with Apple private key is not PEM")
	}
	var parsed any
	var err error
	switch block.Type {
	case "PRIVATE KEY":
		parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		parsed, err = x509.ParseECPrivateKey(block.Bytes)
	default:
		return nil, errors.New("oauth: the Sign in with Apple private key is not a private key block")
	}
	if err != nil {
		return nil, errors.New("oauth: the Sign in with Apple private key could not be parsed")
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("oauth: the Sign in with Apple private key is not an EC key on P-256")
	}
	return key, nil
}

// appleClientSecret is the JWT Apple takes as the client secret: ES256, signed
// with the key Apple issued, naming the team as issuer and the Services ID as
// subject.
func appleClientSecret(key *ecdsa.PrivateKey, teamID, servicesID, keyID string, now time.Time) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "ES256", "kid": keyID, "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{
		"iss": teamID,
		"iat": now.Unix(),
		"exp": now.Add(appleSecretLifetime).Unix(),
		"aud": appleAudience,
		"sub": servicesID,
	})
	if err != nil {
		return "", err
	}
	signed := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)

	digest := sha256.Sum256([]byte(signed))
	der, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		return "", errors.New("oauth: signing the Sign in with Apple client secret failed")
	}
	// JWS wants r and s as two fixed 32-byte numbers side by side; SignASN1
	// answers a DER sequence of two integers of whatever length they have.
	var sig struct{ R, S *big.Int }
	if rest, err := asn1.Unmarshal(der, &sig); err != nil || len(rest) != 0 {
		return "", errors.New("oauth: the signature of the Sign in with Apple client secret is malformed")
	}
	raw := make([]byte, 64)
	sig.R.FillBytes(raw[:32])
	sig.S.FillBytes(raw[32:])
	return signed + "." + base64.RawURLEncoding.EncodeToString(raw), nil
}

// appleCallbackUser adds the name Apple posts with the first authorization.
//
// The field is JSON, {"name":{"firstName":"..","lastName":".."},"email":".."},
// and arrives from the browser, so it is read as what a person typed: bounded,
// used for the name only, and ignored when it does not parse. The answer is a
// copy; the claims' own values are not changed.
func appleCallbackUser(r *http.Request, user oauth.UserData) oauth.UserData {
	field := hhttp.NewRequest(r).String("user")
	if field == "" || len(field) > appleUserMaxBytes {
		return user
	}
	var posted struct {
		Name struct {
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
		} `json:"name"`
	}
	if err := json.Unmarshal([]byte(field), &posted); err != nil {
		return user
	}
	given := strings.TrimSpace(posted.Name.FirstName)
	family := strings.TrimSpace(posted.Name.LastName)
	if given == "" && family == "" {
		return user
	}

	params := user.All()
	out := make(map[string]any, len(params)+3)
	for k, v := range params {
		out[k] = v
	}
	out["given_name"] = given
	out["family_name"] = family
	out["name"] = strings.TrimSpace(given + " " + family)
	return oauth.NewUserData(out)
}
