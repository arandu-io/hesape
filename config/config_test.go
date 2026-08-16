package config_test

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/config"
	"github.com/arandu-io/hesape/encryption"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func validApp(t *testing.T) config.App {
	t.Helper()
	return config.App{
		Name:     "test",
		Env:      config.EnvDev,
		HTTPAddr: ":8080",
		URL:      mustURL(t, "http://localhost:8080"),
		Timezone: time.UTC,
		Locale:   "en",
		Key:      make([]byte, encryption.KeySize),
	}
}

func TestValidateAcceptsAValidApp(t *testing.T) {
	if err := validApp(t).Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsUnknownEnv(t *testing.T) {
	app := validApp(t)
	app.Env = "production" // a common and expensive typo: it is "prod"

	err := app.Validate()
	if err == nil {
		t.Fatal("an unknown APP_ENV was accepted, which would silently enable the debug page")
	}
	if !strings.Contains(err.Error(), "dev, staging or prod") {
		t.Errorf("the message must list the valid values, got: %v", err)
	}
}

// TestValidateRejectsWrongKeyLength also checks that the message names the
// command that fixes it -- an error that only states the problem costs a search.
func TestValidateRejectsWrongKeyLength(t *testing.T) {
	app := validApp(t)
	app.Key = []byte("too-short")

	err := app.Validate()
	if err == nil {
		t.Fatal("a short APP_KEY was accepted")
	}
	if !strings.Contains(err.Error(), "aru key:generate") {
		t.Errorf("the message must name the fix, got: %v", err)
	}
}

// TestValidateRejectsAShortPreviousKey: a retired key of the wrong length does
// not fail on boot, it fails on the one request that carries a cookie signed
// before the rotation -- which is a logout nobody can reproduce.
func TestValidateRejectsAShortPreviousKey(t *testing.T) {
	app := validApp(t)
	app.PreviousKeys = [][]byte{make([]byte, encryption.KeySize), []byte("short")}

	err := app.Validate()
	if err == nil {
		t.Fatal("a short entry in APP_PREVIOUS_KEYS was accepted")
	}
	if !strings.Contains(err.Error(), "entry 2") {
		t.Errorf("the message must name which entry is wrong, got: %v", err)
	}
}

// TestValidateRejectsDebugInProduction: the debug page prints the stack, the
// request and the environment, so leaving it on in production is a disclosure
// of every secret the process holds.
func TestValidateRejectsDebugInProduction(t *testing.T) {
	app := validApp(t)
	app.Env = config.EnvProd
	app.Debug = true

	if err := app.Validate(); err == nil {
		t.Fatal("APP_DEBUG=true was accepted in production")
	}
}

func TestValidateRequiresAnAbsoluteURL(t *testing.T) {
	for name, raw := range map[string]string{
		"a path":         "/billing",
		"a bare host":    "billing.example.com",
		"scheme only":    "https://",
		"nothing at all": "",
	} {
		t.Run(name, func(t *testing.T) {
			app := validApp(t)
			app.URL = mustURL(t, raw)

			if err := app.Validate(); err == nil {
				t.Fatalf("APP_URL=%q was accepted: a job would build links against it", raw)
			}
		})
	}
}

func TestValidateRequiresAnAddressOutsideDevelopment(t *testing.T) {
	app := validApp(t)
	app.Env = config.EnvProd
	app.HTTPAddr = ""

	if err := app.Validate(); err == nil {
		t.Fatal("an empty HTTP_ADDR was accepted outside development")
	}
}

func TestEnvIsAndIsProduction(t *testing.T) {
	for env, wantProduction := range map[config.Env]bool{
		config.EnvDev:     false,
		config.EnvStaging: false,
		config.EnvProd:    true,
	} {
		if got := env.IsProduction(); got != wantProduction {
			t.Errorf("IsProduction for %q = %v, want %v", env, got, wantProduction)
		}
		if !env.Is(env) {
			t.Errorf("Is(%q) on itself is false", env)
		}
		if env.Is("production") {
			t.Errorf("%q matched the literal \"production\", which is not an environment here", env)
		}
	}
}

// TestLoadDecodesBase64Key covers the format `aru key:generate` emits: 32
// random bytes are not printable, so the value in.env is always encoded.
func TestLoadDecodesBase64Key(t *testing.T) {
	emptyProject(t)
	raw := make([]byte, encryption.KeySize)
	for i := range raw {
		raw[i] = byte(i)
	}
	t.Setenv("APP_KEY", "base64:"+base64.StdEncoding.EncodeToString(raw))

	app, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(app.Key) != encryption.KeySize {
		t.Fatalf("key length = %d, want %d", len(app.Key), encryption.KeySize)
	}
	if string(app.Key) != string(raw) {
		t.Fatal("the decoded key does not match what was encoded")
	}
}

func TestLoadRejectsBrokenBase64Key(t *testing.T) {
	emptyProject(t)
	t.Setenv("APP_KEY", "base64:not!valid!base64")

	if _, err := config.Load(); err == nil {
		t.Fatal("a malformed base64 APP_KEY was accepted")
	}
}

// TestLoadReadsPreviousKeys: rotation is the reason the field exists, and the
// order is load-bearing -- newest first, so the common case is the first try.
func TestLoadReadsPreviousKeys(t *testing.T) {
	emptyProject(t)
	older := strings.Repeat("o", encryption.KeySize)
	oldest := strings.Repeat("z", encryption.KeySize)

	t.Setenv("APP_KEY", strings.Repeat("k", encryption.KeySize))
	t.Setenv("APP_PREVIOUS_KEYS", older+", base64:"+base64.StdEncoding.EncodeToString([]byte(oldest)))

	app, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(app.PreviousKeys) != 2 {
		t.Fatalf("PreviousKeys has %d entries, want 2", len(app.PreviousKeys))
	}
	if string(app.PreviousKeys[0]) != older {
		t.Error("the first retired key is not the one written first: rotation order is not preserved")
	}
	if string(app.PreviousKeys[1]) != oldest {
		t.Error("the base64 form was not decoded in APP_PREVIOUS_KEYS")
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	emptyProject(t)
	t.Setenv("APP_KEY", strings.Repeat("k", encryption.KeySize))

	app, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if app.Env != config.EnvDev {
		t.Errorf("default Env = %q, want dev", app.Env)
	}
	if app.HTTPAddr != ":8080" {
		t.Errorf("default HTTPAddr = %q, want :8080", app.HTTPAddr)
	}
	if app.URL.String() != "http://localhost:8080" {
		t.Errorf("default URL = %q, want http://localhost:8080", app.URL)
	}
	if app.Timezone != time.UTC {
		t.Errorf("default Timezone = %v, want UTC: storage is UTC and rendering must not drift from it by default", app.Timezone)
	}
	if app.Locale != "en" {
		t.Errorf("default Locale = %q, want en", app.Locale)
	}
	if len(app.PreviousKeys) != 0 {
		t.Error("a fresh project has no retired keys")
	}
}

// TestDebugDefaultsToTheEnvironment is the property that keeps the error page
// out of production without anyone having to remember a variable.
func TestDebugDefaultsToTheEnvironment(t *testing.T) {
	for env, want := range map[config.Env]bool{
		config.EnvDev:     true,
		config.EnvStaging: false,
		config.EnvProd:    false,
	} {
		t.Run(string(env), func(t *testing.T) {
			emptyProject(t)
			t.Setenv("APP_KEY", strings.Repeat("k", encryption.KeySize))
			t.Setenv("APP_ENV", string(env))

			app, err := config.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if app.Debug != want {
				t.Errorf("Debug in %q = %v, want %v", env, app.Debug, want)
			}
		})
	}
}

func TestLoadRejectsAnUnknownTimezone(t *testing.T) {
	emptyProject(t)
	t.Setenv("APP_KEY", strings.Repeat("k", encryption.KeySize))
	t.Setenv("APP_TIMEZONE", "Middle/Earth")

	if _, err := config.Load(); err == nil {
		t.Fatal("an unknown APP_TIMEZONE was accepted: every rendered date would silently be UTC")
	}
}
