package broadcasting

import (
	"context"
	"net/http"

	"github.com/arandu-io/hesape/auth"
)

// Broadcaster is the three methods every driver has.
//
// The implementations live in
// github.com/arandu-io/hesape/broadcasting/broadcasters. The contract stays
// here because the package that implements it imports this one.
type Broadcaster interface {
	// Auth decides whether the caller may listen on a channel, and answers the
	// body the client is sent back.
	//
	// The Grant comes back beside the response, and that is the whole point of
	// this framework: a private channel is an authorization decision, the
	// decision is made by a Policy, and the Grant is the proof it happened.
	// auth.ErrForbidden is the refusal, and nothing scoped by tenant is
	// reachable without the Grant -- including the name the event is finally
	// published under, which is [TenantChannel](grant, channel).
	//
	// The subject is not an argument. It comes from the context, where the
	// middleware that loaded the session put it -- auth.SubjectFrom(ctx) --
	// which, unlike a parameter, cannot be supplied by the caller being
	// authorized.
	//
	// The channel is the raw name the client asked for, prefix and all:
	// "private-orders.17". The driver normalizes it, and refuses it outright if
	// it names a tenant -- see [RequestedChannel].
	//
	// The Grant is the answer, not the response. A driver that authorized
	// nobody answers the zero Grant, and the zero Grant fails
	// auth.Grant.Check([ChannelJoin]); a caller that reads only the response is
	// reading a body a driver may have produced without deciding anything.
	Auth(ctx context.Context, channel string) (auth.Grant, any, error)

	// ValidAuthenticationResponse is the body a successful Auth answers with,
	// built from whatever the channel handler returned.
	//
	// It takes the Grant rather than the request, because everything it needs --
	// who the subject is, which tenant they are in -- is on the Grant, and
	// reading it there is the difference between a response about the person who
	// was authorized and a response about whoever sent the bytes.
	//
	// channel is the channel the client asked for, without a tenant. It is a
	// parameter because the answer has to say what it is an answer about: a
	// relay given a bare `true` signs the socket onto the string the client
	// sent. The implementations name the channel out of [TenantChannel](g,
	// channel) and put that name in the body, which is why this method cannot be
	// called without a Grant.
	ValidAuthenticationResponse(ctx context.Context, g auth.Grant, channel Channel, result any) (any, error)

	// Broadcast publishes the event.
	//
	// ctx is there because this is I/O. Every channel goes out under
	// [TenantChannel](g, channel), so the tenant is in the name of everything
	// published and a subscriber in one tenant cannot name a channel in another.
	Broadcast(ctx context.Context, g auth.Grant, channels []Channel, event string, payload map[string]any) error
}

// UserResolver is the optional fourth method of a driver: it answers who the
// connection belongs to.
//
// [BroadcastController.AuthenticateUser] asks the driver for it, and refuses
// with 403 when the driver does not have it.
type UserResolver interface {
	// ResolveAuthenticatedUser is the user payload the socket server is given
	// for the connection, or nil when no callback was registered.
	ResolveAuthenticatedUser(ctx context.Context, r *http.Request) (any, error)
}
