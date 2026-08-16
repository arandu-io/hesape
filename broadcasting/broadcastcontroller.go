package broadcasting

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/arandu-io/hesape/auth"
)

// ChannelNameField is the field the socket client sends the channel it wants to
// listen on under.
const ChannelNameField = "channel_name"

// BroadcastController is the two endpoints a socket client calls before it is
// allowed to listen.
type BroadcastController struct {
	// broadcast is the manager the driver is resolved through.
	broadcast *BroadcastManager
}

// NewBroadcastController builds the controller over a manager.
func NewBroadcastController(broadcast *BroadcastManager) *BroadcastController {
	return &BroadcastController{broadcast: broadcast}
}

// Authenticate authorizes the request for channel access.
//
// This is where a private channel becomes an authorization decision. The
// channel name arrives from the client, the subject arrives from the context
// where the session middleware put it, and the driver's Auth runs the channel's
// Policy through auth.Authorize -- so a refusal is auth.ErrForbidden and a
// success is a Grant, exactly as it is on the way into a repository. The client
// never names the tenant: it comes off the Grant.
//
// The refusal is 403, and the body is deliberately the same sentence for every
// refusal: the reason a channel was denied is a fact about somebody else's data.
//
// The Grant is checked for [ChannelJoin] before anything is written. A driver
// that decides nothing answers the zero Grant with no error at all -- which is
// what the log and null drivers do -- and the check is what keeps that from
// reading as a success.
func (c *BroadcastController) Authenticate(w http.ResponseWriter, r *http.Request) {
	driver, err := c.broadcast.Driver("")
	if err != nil {
		http.Error(w, "Broadcasting is not configured.", http.StatusInternalServerError)

		return
	}

	channel := r.FormValue(ChannelNameField)

	g, response, err := driver.Auth(r.Context(), channel)
	if err != nil {
		http.Error(w, "This action is unauthorized.", http.StatusForbidden)

		return
	}
	// The Grant is what says a Policy ran and whose channel this is. A driver
	// that decided nothing answers the zero Grant, and it never reaches a 200.
	if err := g.Check(ChannelJoin); err != nil {
		http.Error(w, "This action is unauthorized.", http.StatusForbidden)

		return
	}

	writeAuthResponse(w, response)
}

// AuthenticateUser authenticates the current user for the connection itself,
// rather than for one channel.
//
// See https://pusher.com/docs/channels/server_api/authenticating-users for the
// document the client expects.
//
// A driver that resolves no user is a 403. So is a driver that has no resolver
// at all: the endpoint exists to answer who somebody is, and a deployment that
// never registered a way to say cannot answer.
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
// A driver that already produced a JSON document is written through untouched,
// because encoding it again would ship the client a quoted string.
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
