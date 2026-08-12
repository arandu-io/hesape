package auth_test

import (
	"testing"

	"github.com/arandu-io/hesape/auth"
)

func TestAGenericUserIsTheRowItWasBuiltFrom(t *testing.T) {
	user := auth.NewGenericUser(map[string]any{
		"id":             12,
		"email":          "person@example.com",
		"password":       "hashed:secret",
		"remember_token": "the-token",
	})

	if user.GetAuthIdentifierName() != "id" || user.GetAuthIdentifier() != 12 {
		t.Errorf("the identifier is %v under %q", user.GetAuthIdentifier(), user.GetAuthIdentifierName())
	}
	if user.GetAuthPasswordName() != "password" || user.GetAuthPassword() != "hashed:secret" {
		t.Errorf("the password is %q under %q", user.GetAuthPassword(), user.GetAuthPasswordName())
	}
	if user.GetRememberTokenName() != "remember_token" || user.GetRememberToken() != "the-token" {
		t.Errorf("the remember token is %q", user.GetRememberToken())
	}

	user.SetRememberToken("another-token")

	if user.GetRememberToken() != "another-token" {
		t.Errorf("SetRememberToken did not take: %q", user.GetRememberToken())
	}
	if user.Attributes["remember_token"] != "another-token" {
		t.Error("the token was not written back to the row")
	}
}

func TestAGenericUserReachesTheColumnsThatAreNotContractMethods(t *testing.T) {
	user := auth.NewGenericUser(map[string]any{"id": 1, "name": "Ana"})

	if user.Get("name") != "Ana" {
		t.Errorf("Get answered %v", user.Get("name"))
	}
	if user.Get("nothing") != nil {
		t.Errorf("Get invented a column: %v", user.Get("nothing"))
	}

	// __set, __isset and __unset are the map itself.
	user.Attributes["name"] = "Bruno"
	if user.Get("name") != "Bruno" {
		t.Error("writing a column did not take")
	}
	if _, ok := user.Attributes["name"]; !ok {
		t.Error("the column is not there after it was written")
	}
	delete(user.Attributes, "name")
	if _, ok := user.Attributes["name"]; ok {
		t.Error("the column survived being dropped")
	}
}

func TestAGenericUserWithNoRowAnswersEmptyRatherThanPanicking(t *testing.T) {
	user := auth.NewGenericUser(nil)

	if user.GetAuthIdentifier() != nil {
		t.Errorf("the identifier is %v, want nil", user.GetAuthIdentifier())
	}
	if user.GetAuthPassword() != "" || user.GetRememberToken() != "" {
		t.Error("a user with no row has a password or a token")
	}

	user.SetRememberToken("the-token")

	if user.GetRememberToken() != "the-token" {
		t.Error("the token could not be written to a user with no row")
	}
}
