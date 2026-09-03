package session_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/arandu-io/hesape/session"
)

// safe is the smallest configuration that Config.Check accepts, and the base
// every case below changes one field of.
func safe() session.Config {
	return session.Config{
		Driver:   "array",
		Cookie:   "arandu_session",
		HTTPOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

// TestCheckRefusesTheZeroValueAndNamesTheField fixes the refusal a zero-value
// configuration gets.
//
// The zero value is what a struct literal that lists the driver and nothing else
// produces, and it is the configuration this package used to accept: no
// HTTPOnly, no SameSite, no Secure. The error names HTTPOnly because that is the
// first field read, and the exact name is the assertion -- a refusal that says
// only "unsafe" sends a person to read the source to find out which of four
// fields it meant.
func TestCheckRefusesTheZeroValueAndNamesTheField(t *testing.T) {
	err := session.Config{}.Check()
	if err == nil {
		t.Fatal("a zero-value Config passed the check, and it configures a session id that any script on the page can read")
	}
	if !errors.Is(err, session.ErrUnsafeConfig) {
		t.Errorf("the refusal does not wrap ErrUnsafeConfig, so a caller has to match on the message: %v", err)
	}

	var refusal *session.ConfigError
	if !errors.As(err, &refusal) {
		t.Fatalf("the refusal is not a *ConfigError, so nothing can read which field it means: %T", err)
	}
	if refusal.Field != "HTTPOnly" {
		t.Errorf("the refusal names %q, want HTTPOnly", refusal.Field)
	}
}

// TestCheckRefusesEachUnsafeField walks the fields one at a time from a
// configuration that passes, so every case fails for the reason it names rather
// than for whichever field happens to be read first.
func TestCheckRefusesEachUnsafeField(t *testing.T) {
	cases := []struct {
		name  string
		cfg   func(session.Config) session.Config
		field string
	}{
		{
			name:  "the session id is readable by script",
			cfg:   func(c session.Config) session.Config { c.HTTPOnly = false; return c },
			field: "HTTPOnly",
		},
		{
			name:  "no cross-site policy is stated",
			cfg:   func(c session.Config) session.Config { c.SameSite = 0; return c },
			field: "SameSite",
		},
		{
			name: "SameSite None without Secure, which a browser drops",
			cfg: func(c session.Config) session.Config {
				c.SameSite = http.SameSiteNoneMode
				c.Secure = false
				return c
			},
			field: "Secure",
		},
		{
			name: "a deployment domain served in the clear",
			cfg: func(c session.Config) session.Config {
				c.Domain = "app.example.com"
				c.Secure = false
				return c
			},
			field: "Secure",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var refusal *session.ConfigError
			err := tc.cfg(safe()).Check()
			if !errors.As(err, &refusal) {
				t.Fatalf("Check() = %v, want a *ConfigError naming %s", err, tc.field)
			}
			if refusal.Field != tc.field {
				t.Errorf("the refusal names %q, want %q", refusal.Field, tc.field)
			}
		})
	}
}

// TestCheckAcceptsWhatItShould fixes the other side, because a check that
// refused everything would satisfy every test above and configure nothing.
//
// The loopback case is the one worth naming: a cookie without Secure on
// localhost goes nowhere a third party is, so refusing it would only teach a
// developer to turn the check off.
func TestCheckAcceptsWhatItShould(t *testing.T) {
	cases := map[string]session.Config{
		"the smallest safe configuration": safe(),
		"loopback served in the clear": func() session.Config {
			c := safe()
			c.Domain = "localhost"
			return c
		}(),
		"loopback written with a leading dot": func() session.Config {
			c := safe()
			c.Domain = ".127.0.0.1"
			return c
		}(),
		"a deployment domain over TLS": func() session.Config {
			c := safe()
			c.Domain = "app.example.com"
			c.Secure = true
			return c
		}(),
		"SameSite None with Secure": func() session.Config {
			c := safe()
			c.SameSite = http.SameSiteNoneMode
			c.Secure = true
			return c
		}(),
		"SameSite Strict": func() session.Config {
			c := safe()
			c.SameSite = http.SameSiteStrictMode
			return c
		}(),
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if err := cfg.Check(); err != nil {
				t.Errorf("Check() = %v, want nil", err)
			}
		})
	}
}

// TestSameSiteAndHTTPOnlyHaveOneAcceptedValueEach fixes the values the check
// settles on, so changing them is a change to this test and not a change
// somebody notices in a browser.
//
// HTTPOnly has exactly one accepted value. SameSite has three, and the zero
// value -- which is the one a struct literal produces -- is not among them.
func TestSameSiteAndHTTPOnlyHaveOneAcceptedValueEach(t *testing.T) {
	for _, httpOnly := range []bool{false, true} {
		cfg := safe()
		cfg.HTTPOnly = httpOnly
		if accepted := cfg.Check() == nil; accepted != httpOnly {
			t.Errorf("HTTPOnly=%v accepted=%v, and true is the only value that may be accepted", httpOnly, accepted)
		}
	}

	accepted := map[http.SameSite]bool{
		http.SameSiteLaxMode:    true,
		http.SameSiteStrictMode: true,
		http.SameSiteNoneMode:   true,
	}
	for mode := http.SameSite(0); mode <= http.SameSiteNoneMode; mode++ {
		cfg := safe()
		cfg.SameSite = mode
		cfg.Secure = true // None needs it, and the other modes do not mind.
		if got := cfg.Check() == nil; got != accepted[mode] {
			t.Errorf("SameSite=%d accepted=%v, want %v", mode, got, accepted[mode])
		}
	}
}
