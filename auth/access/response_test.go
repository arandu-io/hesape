package access_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/access"
)

func TestAllowAndDenyAnswerOppositeQuestions(t *testing.T) {
	allowed := access.Allow("welcome", "greeting")
	if !allowed.Allowed() || allowed.Denied() {
		t.Fatalf("Allow: allowed=%v denied=%v, want true/false", allowed.Allowed(), allowed.Denied())
	}
	if allowed.Message() != "welcome" {
		t.Fatalf("Message = %q, want welcome", allowed.Message())
	}
	if allowed.Code() != "greeting" {
		t.Fatalf("Code = %v, want greeting", allowed.Code())
	}

	denied := access.Deny("not yours", nil)
	if denied.Allowed() || !denied.Denied() {
		t.Fatalf("Deny: allowed=%v denied=%v, want false/true", denied.Allowed(), denied.Denied())
	}
	if denied.Status() != nil {
		t.Fatalf("Status = %v, want nil on a plain denial", *denied.Status())
	}
}

func TestDenyWithStatusAndDenyAsNotFoundCarryTheStatus(t *testing.T) {
	teapot := access.DenyWithStatus(http.StatusTeapot, "no coffee", nil)
	if teapot.Status() == nil || *teapot.Status() != http.StatusTeapot {
		t.Fatalf("DenyWithStatus status = %v, want 418", teapot.Status())
	}

	hidden := access.DenyAsNotFound("no invoice with that number", nil)
	if hidden.Status() == nil || *hidden.Status() != http.StatusNotFound {
		t.Fatalf("DenyAsNotFound status = %v, want 404", hidden.Status())
	}
	if !hidden.Denied() {
		t.Fatal("DenyAsNotFound was allowed")
	}
}

func TestWithStatusAndAsNotFoundSetTheStatusOnTheResponse(t *testing.T) {
	response := access.Deny("", nil).AsNotFound()
	if response.Status() == nil || *response.Status() != http.StatusNotFound {
		t.Fatalf("AsNotFound status = %v, want 404", response.Status())
	}

	if response.WithStatus(nil).Status() != nil {
		t.Fatal("WithStatus(nil) did not clear the status")
	}
}

func TestAuthorizeReturnsTheResponseWhenItWasAllowed(t *testing.T) {
	allowed := access.Allow("go ahead", nil)

	response, err := allowed.Authorize()
	if err != nil {
		t.Fatalf("Authorize on an allowed response: %v", err)
	}
	if response != allowed {
		t.Fatal("Authorize did not hand back the same response")
	}
}

func TestAuthorizeFailsWithTheResponseAttachedWhenItWasDenied(t *testing.T) {
	denied := access.DenyAsNotFound("no post with that id", "post.missing")

	response, err := denied.Authorize()
	if err == nil {
		t.Fatal("Authorize on a denied response returned no error")
	}
	if response != nil {
		t.Fatal("Authorize returned a response alongside the error")
	}

	var authorization *access.AuthorizationError
	if !errors.As(err, &authorization) {
		t.Fatalf("error = %T, want *access.AuthorizationError", err)
	}
	if authorization.Response() != denied {
		t.Fatal("the error does not carry the response that produced it")
	}
	if !authorization.HasStatus() || *authorization.Status() != http.StatusNotFound {
		t.Fatalf("error status = %v, want 404", authorization.Status())
	}
	if authorization.Code() != "post.missing" {
		t.Fatalf("error code = %v, want post.missing", authorization.Code())
	}
	if authorization.Error() != "no post with that id" {
		t.Fatalf("error message = %q, want the response message", authorization.Error())
	}
}

func TestADeniedResponseIsForbidden(t *testing.T) {
	_, err := access.Deny("", nil).Authorize()

	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("error = %v, want auth.ErrForbidden -- the exception handler answers 403 on that alone", err)
	}
}

func TestToArrayAndStringReportTheResponse(t *testing.T) {
	response := access.Deny("not yours", 7)

	array := response.ToArray()
	if array["allowed"] != false || array["message"] != "not yours" || array["code"] != 7 {
		t.Fatalf("ToArray = %v", array)
	}

	if response.String() != "not yours" {
		t.Fatalf("String = %q, want the message", response.String())
	}
}

func TestAuthorizationErrorFallsBackToTheDefaultMessageAndCode(t *testing.T) {
	err := access.NewAuthorizationError("", nil, nil)

	if err.Error() != access.DefaultDenialMessage {
		t.Fatalf("message = %q, want %q", err.Error(), access.DefaultDenialMessage)
	}
	if err.Code() != 0 {
		t.Fatalf("code = %v, want 0", err.Code())
	}
	if err.HasStatus() {
		t.Fatal("a fresh error reports a status")
	}
}

func TestAuthorizationErrorCarriesItsCauseAndItsStatus(t *testing.T) {
	cause := errors.New("the row was gone")

	err := access.NewAuthorizationError("no post with that id", "post.missing", cause).AsNotFound()

	if !errors.Is(err, cause) {
		t.Fatal("the cause is not reachable through errors.Is")
	}
	if !err.HasStatus() || *err.Status() != http.StatusNotFound {
		t.Fatalf("status = %v, want 404", err.Status())
	}

	response := err.ToResponse()
	if !response.Denied() {
		t.Fatal("ToResponse was allowed")
	}
	if response.Message() != "no post with that id" || response.Code() != "post.missing" {
		t.Fatalf("ToResponse = %v", response.ToArray())
	}
	if response.Status() == nil || *response.Status() != http.StatusNotFound {
		t.Fatalf("ToResponse status = %v, want 404", response.Status())
	}
}

func TestHandlesAuthorizationDeniesFromAPolicyThatEmbedsIt(t *testing.T) {
	var policy access.HandlesAuthorization

	gone := policy.DenyAsNotFound("no post with that id", nil)
	if !gone.Denied() || gone.Status() == nil || *gone.Status() != http.StatusNotFound {
		t.Fatalf("DenyAsNotFound = %v, status %v", gone.ToArray(), gone.Status())
	}

	teapot := policy.DenyWithStatus(http.StatusTeapot, "no coffee", "coffee.none")
	if !teapot.Denied() || teapot.Status() == nil || *teapot.Status() != http.StatusTeapot {
		t.Fatalf("DenyWithStatus = %v, status %v", teapot.ToArray(), teapot.Status())
	}

	// Gate embeds HandlesAuthorization too.
	if !access.NewGate().DenyAsNotFound("", nil).Denied() {
		t.Fatal("the Gate does not carry the trait")
	}
}
