package auth_test

import (
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
)

func TestAnAuthenticationErrorCarriesTheGuardsThatWereAsked(t *testing.T) {
	err := auth.NewAuthenticationError("", []string{"web", "admin"}, "")

	if err.Error() != "Unauthenticated." {
		t.Errorf("the message is %q, want the PHP's default", err.Error())
	}
	if guards := err.Guards(); len(guards) != 2 || guards[0] != "web" {
		t.Errorf("the guards are %v", err.Guards())
	}

	var target *auth.AuthenticationError
	if !errors.As(error(err), &target) {
		t.Error("errors.As does not find it, so a middleware cannot read the guards off it")
	}
}

func TestTheRedirectIsTheErrorsOwnBeforeTheStaticCallback(t *testing.T) {
	auth.RedirectUsing(nil)
	defer auth.RedirectUsing(nil)

	err := auth.NewAuthenticationError("Unauthenticated.", nil, "/sign-in")

	if err.RedirectTo(nil) != "/sign-in" {
		t.Errorf("RedirectTo answered %q", err.RedirectTo(nil))
	}

	auth.RedirectUsing(func(auth.Request) string { return "/elsewhere" })

	if err.RedirectTo(nil) != "/sign-in" {
		t.Error("the callback overrode a redirect the error carried")
	}

	withoutOwn := auth.NewAuthenticationError("", nil, "")

	if withoutOwn.RedirectTo(nil) != "/elsewhere" {
		t.Errorf("the callback was not used: %q", withoutOwn.RedirectTo(nil))
	}
}

func TestWithNoRedirectAtAllThereIsNoPageToSendAnybodyTo(t *testing.T) {
	auth.RedirectUsing(nil)

	err := auth.NewAuthenticationError("", nil, "")

	if err.RedirectTo(nil) != "" {
		t.Errorf("RedirectTo invented %q, and an API client would be redirected to it", err.RedirectTo(nil))
	}
}
