package broadcasting

import (
	"context"
	"net/http"

	"github.com/arandu-io/hesape/auth"
)

// Broadcaster is Illuminate\Contracts\Broadcasting\Broadcaster: the three
// methods every driver has.
//
// The implementations live in
// github.com/arandu-io/hesape/broadcasting/broadcasters, which is where
// Illuminate\Broadcasting\Broadcasters is. This interface stays here because
// Illuminate\Contracts is where the contract is, and because the package that
// implements it imports this one.
type Broadcaster interface {
	// Auth is Broadcaster::auth: it decides whether the caller may listen on a
	// channel, and answers the body the client is sent back.
	//
	// The PHP returns one value, the authentication response, and throws
	// AccessDeniedHttpException otherwise. Here the Grant comes back beside the
	// response, and that is the whole point of this framework: a private channel
	// is an authorization decision, the decision is made by a Policy, and the
	// Grant is the proof it happened. auth.ErrForbidden is the refusal, and
	// nothing scoped by tenant is reachable without the Grant -- including the
	// name the event is finally published under, which is
	// [TenantChannel](grant, channel).
	//
	// The subject is not an argument. It comes from the context, where the
	// middleware that loaded the session put it -- auth.SubjectFrom(ctx) --
	// which is the Go shape of the PHP's $request->user() and, unlike a
	// parameter, cannot be supplied by the caller being authorized.
	//
	// The channel is the raw name the client asked for, prefix and all:
	// "private-orders.17". The driver normalizes it.
	Auth(ctx context.Context, channel string) (auth.Grant, any, error)

	// ValidAuthenticationResponse is Broadcaster::validAuthenticationResponse:
	// the body a successful Auth answers with, built from whatever the channel
	// handler returned.
	//
	// The Grant replaces the PHP's $request, because everything this method
	// needs off the request -- who the subject is, which tenant they are in --
	// is on the Grant, and reading it there is the difference between a response
	// about the person who was authorized and a response about whoever sent the
	// bytes.
	ValidAuthenticationResponse(ctx context.Context, g auth.Grant, result any) (any, error)

	// Broadcast is Broadcaster::broadcast: it publishes the event.
	//
	// ctx is the first mechanical change of ADR 0044 -- this is I/O. g is
	// RULE 14: every channel goes out under [TenantChannel](g, channel), so the
	// tenant is in the name of everything published and a subscriber in one
	// tenant cannot name a channel in another.
	Broadcast(ctx context.Context, g auth.Grant, channels []Channel, event string, payload map[string]any) error
}

// UserResolver is Broadcaster::resolveAuthenticatedUser, which the abstract
// Broadcaster provides and the contract does not.
//
// BroadcastController.AuthenticateUser asks the driver for it and refuses when
// the driver does not answer, which is what the PHP's `?? throw new
// AccessDeniedHttpException` does for a null.
type UserResolver interface {
	// ResolveAuthenticatedUser is resolveAuthenticatedUser($request): the user
	// payload the socket server is given for the connection, or nil when no
	// callback was registered.
	ResolveAuthenticatedUser(ctx context.Context, r *http.Request) (any, error)
}
