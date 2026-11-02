package oauth_test

import (
	"reflect"
	"testing"

	"github.com/arandu-io/hesape/oauth"
)

func TestUserDataKeepsWhatTheProviderSent(t *testing.T) {
	user := oauth.NewUserData(map[string]any{
		"id":       float64(1234567),
		"login":    "grace",
		"verified": true,
		"name":     nil,
	})

	if !user.Has("login") || user.Has("nickname") {
		t.Fatal("Has answered for the wrong keys")
	}
	if got := user.Get("login"); got != "grace" {
		t.Fatalf("Get(login) = %v, want grace", got)
	}
	if got := user.Get("nickname", "none"); got != "none" {
		t.Fatalf("Get with a fallback = %v, want none", got)
	}
	if got := user.Keys(); !reflect.DeepEqual(got, []string{"id", "login", "name", "verified"}) {
		t.Fatalf("Keys = %v, want them sorted", got)
	}
	if len(user.All()) != 4 {
		t.Fatalf("All has %d keys, want 4", len(user.All()))
	}
}

// TestStringConvertsWhatJSONMadeOfIt: an id is a JSON number, and a number
// printed carelessly reaches the database as 1.234567e+06.
func TestStringConvertsWhatJSONMadeOfIt(t *testing.T) {
	user := oauth.NewUserData(map[string]any{
		"id":       float64(1234567),
		"score":    2.5,
		"verified": true,
		"name":     nil,
	})

	for key, want := range map[string]string{
		"id":       "1234567",
		"score":    "2.5",
		"verified": "true",
		"name":     "",
		"missing":  "",
	} {
		if got := user.String(key); got != want {
			t.Errorf("String(%s) = %q, want %q", key, got, want)
		}
	}
	if got := user.String("missing", "fallback"); got != "fallback" {
		t.Fatalf("String with a fallback = %q, want fallback", got)
	}
}

func TestTheZeroUserDataIsUsable(t *testing.T) {
	var user oauth.UserData
	if user.Has("anything") || user.String("anything") != "" || len(user.All()) != 0 {
		t.Fatal("the zero UserData must answer as an empty one")
	}
}

func TestUserDataBoolReadsABooleanOrTheTextTrue(t *testing.T) {
	user := oauth.NewUserData(map[string]any{
		"yes":       true,
		"no":        false,
		"text":      "true",
		"loud":      " TRUE ",
		"text no":   "false",
		"one":       float64(1),
		"something": "yes",
		"nothing":   nil,
	})
	for key, want := range map[string]bool{
		"yes": true, "no": false, "text": true, "loud": true,
		"text no": false, "one": false, "something": false, "nothing": false, "absent": false,
	} {
		if got := user.Bool(key); got != want {
			t.Errorf("Bool(%q) = %v, want %v", key, got, want)
		}
	}
	if !user.Bool("absent", true) || !user.Bool("nothing", true) {
		t.Error("the fallback was not used for a key the provider did not send")
	}
	if user.Bool("no", true) {
		t.Error("the fallback replaced a value the provider did send")
	}
}
