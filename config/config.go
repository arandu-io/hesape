package config

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Env is the deployment environment. It gates everything that must never run
// outside development, starting with the debug error page.
type Env string

// Supported environments.
const (
	EnvDev     Env = "dev"
	EnvStaging Env = "staging"
	EnvProd    Env = "prod"
)

// Is reports whether the environment is want.
//
// It exists so a caller reads as a sentence -- cfg.Env.Is(config.EnvStaging) --
// and so the comparison happens in one place if Env ever stops being a bare
// string.
func (e Env) Is(want Env) bool { return e == want }

// IsProduction reports whether this is the environment where a mistake is
// visible to a customer. It is the question asked most often, and spelling it
// out is what keeps EnvProd from being compared against a literal "production"
// -- a typo the type system cannot catch and this method removes the occasion
// for.
func (e Env) IsProduction() bool { return e == EnvProd }

// AppKeyLen is the required length of APP_KEY, in bytes. The key signs session
// cookies, CSRF tokens and signed URLs.
const AppKeyLen = 32

// App is the identity of the application. It is a struct, not a map, and every
// field is validated at boot.
type App struct {
	// Name appears in the page title, in the log and in outgoing mail.
	Name string

	// Env gates everything that must never run outside development.
	Env Env

	// Debug turns on the debug error page -- the one that prints a stack trace,
	// the request and the environment. Validate refuses it in production, where
	// that page is a disclosure and not a convenience.
	Debug bool

	// URL is the canonical address of the application, parsed. It is what builds
	// an absolute link from a job or a scheduled task, where there is no request
	// to read a host from.
	URL *url.URL

	// HTTPAddr is what the server listens on.
	HTTPAddr string

	// Timezone is the location dates are rendered in, resolved. Storage is
	// always UTC; this is presentation only.
	Timezone *time.Location

	// Locale is the default language tag.
	Locale string

	// Key signs session cookies, CSRF tokens and signed URLs. Exactly AppKeyLen
	// bytes.
	Key []byte

	// PreviousKeys are keys retired but still accepted on verification, newest
	// first. They are what makes rotating Key a deploy rather than an outage:
	// signatures issued under the old key keep verifying until they expire.
	// Nothing is ever signed with one.
	PreviousKeys [][]byte
}

// Load reads the environment and validates it. It fails at boot, not on the
// first request.
//
// A .env in the working directory is read first, and it only fills variables
// the environment does not already define -- see [LoadDotenv]. It is read here
// rather than by the application, because a step the application has to
// remember is a step that gets forgotten: this one was, and `aru migrate`
// failed on every new project for it.
func Load() (App, error) {
	if err := LoadDotenv(); err != nil {
		return App{}, err
	}

	key, err := parseKey("APP_KEY", String("APP_KEY", ""))
	if err != nil {
		return App{}, err
	}
	previous, err := parsePreviousKeys(String("APP_PREVIOUS_KEYS", ""))
	if err != nil {
		return App{}, err
	}

	env := Env(String("APP_ENV", string(EnvDev)))

	raw := String("APP_URL", "http://localhost:8080")
	parsed, err := url.Parse(raw)
	if err != nil {
		return App{}, fmt.Errorf("APP_URL is not a URL: %w", err)
	}

	zone := String("APP_TIMEZONE", "UTC")
	location, err := time.LoadLocation(zone)
	if err != nil {
		return App{}, fmt.Errorf("APP_TIMEZONE %q is not a time zone: %w", zone, err)
	}

	app := App{
		Name: String("APP_NAME", "arandu-app"),
		Env:  env,
		// The default is the environment, not false: a developer who has to set
		// a variable to see the error page will read a blank 500 instead, and a
		// default of true would carry the page into production on the first
		// deploy that forgets to unset it. Validate refuses that combination
		// either way.
		Debug:        Bool("APP_DEBUG", env == EnvDev),
		URL:          parsed,
		HTTPAddr:     String("HTTP_ADDR", ":8080"),
		Timezone:     location,
		Locale:       String("APP_LOCALE", "en"),
		Key:          key,
		PreviousKeys: previous,
	}
	return app, app.Validate()
}

// Validate reports the first configuration error, with the command that fixes
// it whenever one exists.
func (a App) Validate() error {
	switch a.Env {
	case EnvDev, EnvStaging, EnvProd:
	default:
		return fmt.Errorf("invalid APP_ENV: %q (expected dev, staging or prod)", a.Env)
	}
	if len(a.Key) != AppKeyLen {
		return fmt.Errorf("APP_KEY must be %d bytes, got %d (run `aru key:generate`)", AppKeyLen, len(a.Key))
	}
	for i, k := range a.PreviousKeys {
		if len(k) != AppKeyLen {
			return fmt.Errorf("APP_PREVIOUS_KEYS entry %d must be %d bytes, got %d", i+1, AppKeyLen, len(k))
		}
	}
	if a.Debug && a.Env.IsProduction() {
		return fmt.Errorf("APP_DEBUG is forbidden in production: the debug page prints the stack, the request and the environment")
	}
	if a.URL == nil || a.URL.Scheme == "" || a.URL.Host == "" {
		return fmt.Errorf("APP_URL must be absolute, with a scheme and a host (for example https://billing.example.com)")
	}
	if a.Timezone == nil {
		return fmt.Errorf("APP_TIMEZONE is required")
	}
	if !a.Env.Is(EnvDev) && a.HTTPAddr == "" {
		return fmt.Errorf("HTTP_ADDR is required outside development")
	}
	return nil
}

// parseKey accepts both a raw 32-byte string and the "base64:" form emitted by
// `aru key:generate` -- a random 32-byte key is not printable, so the generated
// value is always encoded.
//
// The variable name is a parameter because the same format is read from
// APP_KEY and from every entry of APP_PREVIOUS_KEYS, and an error that names
// the wrong one sends the reader to the wrong line of the file.
func parseKey(name, v string) ([]byte, error) {
	if v == "" {
		return nil, nil
	}
	if after, ok := strings.CutPrefix(v, "base64:"); ok {
		b, err := base64.StdEncoding.DecodeString(after)
		if err != nil {
			return nil, fmt.Errorf("%s is not valid base64: %w", name, err)
		}
		return b, nil
	}
	return []byte(v), nil
}

// parsePreviousKeys reads the retired keys, newest first, separated by commas.
//
// Empty entries are skipped rather than rejected, because the value is written
// by prepending: "new,old" is typed as ",old" first often enough that failing
// on it would cost a deploy for nothing.
func parsePreviousKeys(v string) ([][]byte, error) {
	if v == "" {
		return nil, nil
	}
	var keys [][]byte
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, err := parseKey("APP_PREVIOUS_KEYS", part)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}
