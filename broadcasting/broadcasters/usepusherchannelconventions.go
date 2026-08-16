package broadcasters

import (
	"strings"

	"github.com/arandu-io/hesape/broadcasting"
)

// UsePusherChannelConventions is the two questions a driver asks about a
// channel name before it does anything with it.
//
// It is an empty struct a driver embeds, and both methods are promoted onto it.
// The conventions are the Pusher wire format, which is what the socket clients
// on the other end speak, so [RedisBroadcaster] embeds it.
type UsePusherChannelConventions struct{}

// IsGuardedChannel is true when the channel is one that has to be authorized.
//
// The tenant is cut off first. A published channel is named
// "acme:private-orders.17" and does not begin with "private-", so asking
// strings.HasPrefix of the raw wire name answers false for exactly the private
// channels the question protects -- and the subscription then walks past the
// "nobody on the context" refusal underneath it.
func (UsePusherChannelConventions) IsGuardedChannel(channel string) bool {
	_, name, _ := broadcasting.CutTenant(channel)

	return strings.HasPrefix(name, broadcasting.PrivateChannelPrefix) ||
		strings.HasPrefix(name, broadcasting.PresenceChannelPrefix)
}

// NormalizeChannelName is the channel name with its prefix taken off, which is
// the name channels are registered under.
//
// The tenant comes off here too, for the same reason and with a second one: the
// result is what [Broadcaster.VerifyUserCanAccessChannel] matches registered
// patterns against, and a name with the tenant still in it forces an
// application to register "{tenant}:private-orders.{orderId}" -- which makes
// [Broadcaster.ExtractAuthParameters] hand the handler a tenant taken out of
// the request. The pattern an application registers carries no tenant, because
// the name matched against it carries none.
//
// The order matters: private-encrypted- is tried before private-, because it
// starts with it.
func (UsePusherChannelConventions) NormalizeChannelName(channel string) string {
	_, channel, _ = broadcasting.CutTenant(channel)

	for _, prefix := range []string{
		broadcasting.EncryptedPrivateChannelPrefix,
		broadcasting.PrivateChannelPrefix,
		broadcasting.PresenceChannelPrefix,
	} {
		if after, found := strings.CutPrefix(channel, prefix); found {
			return after
		}
	}

	return channel
}
