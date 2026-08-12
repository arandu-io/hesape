package broadcasting

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/arandu-io/hesape/auth"
)

// ChannelNameField is the field the socket client sends the channel it wants to
// listen on under. It is the $request->channel_name RedisBroadcaster reads.
const ChannelNameField = "channel_name"

// BroadcastController is Illuminate\Broadcasting\BroadcastController: the two
// endpoints a socket client calls before it is allowed to listen.
//
// The PHP reaches the manager through the Broadcast facade. There are no
// facades (ADR 0001), so the manager is a field -- which is the only difference
// between the two classes.
type BroadcastController struct {
	// broadcast is what the PHP calls through the Broadcast facade.
	broadcast *BroadcastManager
}

// NewBroadcastController builds the controller over a manager. The PHP has no
// constructor because the facade needs none.
func NewBroadcastController(broadcast *BroadcastManager) *BroadcastController {
	return &BroadcastController{broadcast: broadcast}
}

// Authenticate is BroadcastController::authenticate: it authorizes the request
// for channel access.
//
// This is where a private channel becomes an authorization decision. The
// channel name arrives from the client, the subject arrives from the context
// where the session middleware put it, and the driver's Auth runs the channel's
// Policy through auth.Authorize -- so a refusal is auth.ErrForbidden and a
// success is a Grant, exactly as it is on the way into a repository. The client
// never names the tenant: it comes off the Grant.
//
// The PHP's `$request->session()->reflash()` has no twin. It keeps flash data
// alive across an XHR that the user never sees a page for, and flash data is
// the session package's to manage; this endpoint touches none of it.
//
// The refusal is 403, which is what AccessDeniedHttpException renders to. The
// body is deliberately the same sentence for every refusal: the reason a
// channel was denied is a fact about somebody else's data.
func (c *BroadcastController) Authenticate(w http.ResponseWriter, r *http.Request) {
	driver, err := c.broadcast.Driver("")
	if err != nil {
		http.Error(w, "Broadcasting is not configured.", http.StatusInternalServerError)

		return
	}

	channel := r.FormValue(ChannelNameField)

	_, response, err := driver.Auth(r.Context(), channel)
	if err != nil {
		http.Error(w, "This action is unauthorized.", http.StatusForbidden)

		return
	}

	writeAuthResponse(w, response)
}

// AuthenticateUser is BroadcastController::authenticateUser: it authenticates
// the current user for the connection itself, rather than for one channel.
//
// See https://pusher.com/docs/channels/server_api/authenticating-users, which
// is the link the PHP carries.
//
// A driver that resolves no user is a 403, which is the PHP's
// `?? throw new AccessDeniedHttpException`. So is a driver that has no
// resolver at all: the endpoint exists to answer who somebody is, and a
// deployment that never registered a way to say cannot answer.
func (c *BroadcastController) AuthenticateUser(w http.ResponseWriter, r *http.Request) {
	driver, err := c.broadcast.Driver("")
	if err != nil {
		http.Error(w, "Broadcasting is not configured.", http.StatusInternalServerError)

		return
	}

	resolver, ok := driver.(UserResolver)
	if !ok {
		http.Error(w, "This action is unauthorized.", http.StatusForbidden)

		return
	}

	user, err := resolver.ResolveAuthenticatedUser(r.Context(), r)
	if err != nil || user == nil {
		status := http.StatusForbidden
		if err != nil && !errors.Is(err, auth.ErrForbidden) {
			status = http.StatusInternalServerError
		}
		http.Error(w, "This action is unauthorized.", status)

		return
	}

	writeAuthResponse(w, user)
}

// writeAuthResponse sends what a broadcaster answered.
//
// A driver that already produced a JSON document -- which is what
// RedisBroadcaster::validAuthenticationResponse does with json_encode -- is
// written through untouched, because encoding it again would ship the client a
// quoted string.
func writeAuthResponse(w http.ResponseWriter, response any) {
	w.Header().Set("Content-Type", "application/json")

	if raw, ok := response.(string); ok {
		_, _ = w.Write([]byte(raw))

		return
	}

	encoded, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "This action is unauthorized.", http.StatusForbidden)

		return
	}
	_, _ = w.Write(encoded)
}
