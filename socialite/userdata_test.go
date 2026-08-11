package socialite_test

import (
	"reflect"
	"testing"

	"github.com/arandu-io/hesape/socialite"
)

func TestUserDataKeepsWhatTheProviderSent(t *testing.T) {
	user := socialite.NewUserData(map[string]any{
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
	user := socialite.NewUserData(map[string]any{
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
	var user socialite.UserData
	if user.Has("anything") || user.String("anything") != "" || len(user.All()) != 0 {
		t.Fatal("the zero UserData must answer as an empty one")
	}
}
