package broadcasting_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
)

// answeringBroadcaster is a driver that answers whatever a test hands it, so
// the controller's own reading of the answer is what is under test.
type answeringBroadcaster struct {
	grant    auth.Grant
	response any
	err      error
}

func (b *answeringBroadcaster) Auth(ctx context.Context, channel string) (auth.Grant, any, error) {
	return b.grant, b.response, b.err
}

func (b *answeringBroadcaster) ValidAuthenticationResponse(ctx context.Context, g auth.Grant, channel broadcasting.Channel, result any) (any, error) {
	return b.response, nil
}

func (b *answeringBroadcaster) Broadcast(ctx context.Context, g auth.Grant, channels []broadcasting.Channel, event string, payload map[string]any) error {
	return nil
}

// authenticate posts one subscription at the controller and answers the
// recorder.
func authenticate(t *testing.T, driver broadcasting.Broadcaster) *httptest.ResponseRecorder {
	t.Helper()

	manager := broadcasting.NewBroadcastManager(broadcasting.Config{
		Default:     "answering",
		Connections: map[string]broadcasting.ConnectionConfig{"answering": {Driver: "answering"}},
	}, nil, nil, nil)
	manager.Extend("answering", func(broadcasting.ConnectionConfig) (broadcasting.Broadcaster, error) {
		return driver, nil
	})

	request := httptest.NewRequest(http.MethodPost, broadcasting.AuthRoute, strings.NewReader(broadcasting.ChannelNameField+"=private-orders.17"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	recorder := httptest.NewRecorder()
	broadcasting.NewBroadcastController(manager).Authenticate(recorder, request)

	return recorder
}

// The Grant is the answer, not the response body. A driver that decided nothing
// answers the zero Grant with no error -- which is exactly what
// broadcasters.LogBroadcaster and broadcasters.NullBroadcaster do -- and the
// controller used to read `_, response, err := driver.Auth(...)`, see a nil
// error, and reply 200 to every subscription against either of them.
func TestADriverThatAuthorizedNobodyIsNotA200(t *testing.T) {
	recorder := authenticate(t, &answeringBroadcaster{response: "true"})

	if recorder.Code != http.StatusForbidden {
		t.Errorf("a driver that answered the zero Grant got %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if strings.Contains(recorder.Body.String(), "true") {
		t.Errorf("the refusal carried the driver's success body: %q", recorder.Body.String())
	}
}

// A Grant issued for some other action is not an authorization to listen on a
// channel, and Check is what says so.
func TestAGrantForAnotherActionIsNotAuthorizationToListen(t *testing.T) {
	recorder := authenticate(t, &answeringBroadcaster{
		grant:    auth.SystemGrant("order.delete", "acme"),
		response: "true",
	})

	if recorder.Code != http.StatusForbidden {
		t.Errorf("a Grant for order.delete let a subscription through with %d", recorder.Code)
	}
}

// The success path still answers the driver's document untouched.
func TestAnAuthorizedSubscriptionIsAnsweredWithTheDriversDocument(t *testing.T) {
	document := `{"auth":true,"channel":"acme:private-orders.17"}`

	recorder := authenticate(t, &answeringBroadcaster{
		grant:    auth.SystemGrant(broadcasting.ChannelJoin, "acme"),
		response: document,
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("an authorized subscription got %d, body %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != document {
		t.Errorf("the body is %q, want %q", recorder.Body.String(), document)
	}
}
