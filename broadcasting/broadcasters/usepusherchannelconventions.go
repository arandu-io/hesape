package broadcasters

import (
	"strings"

	"github.com/arandu-io/hesape/broadcasting"
)

// UsePusherChannelConventions is
// Illuminate\Broadcasting\Broadcasters\UsePusherChannelConventions: the two
// questions a driver asks about a channel name before it does anything with it.
//
// The PHP is a trait; here it is an empty struct a driver embeds, and both
// methods are promoted exactly as the trait's are. It stays in this package
// even though Pusher itself does not (reason (3) of ADR 0056): the conventions
// are the wire format of the whole Laravel Echo client, and RedisBroadcaster
// uses the trait in the PHP too.
type UsePusherChannelConventions struct{}

// IsGuardedChannel is UsePusherChannelConventions::isGuardedChannel: true when
// the channel is one that has to be authorized.
//
// The tenant is cut off first, and that is the fix. What was wrong: this method
// asked strings.HasPrefix(channel, "private-") of whatever it was handed, and
// the call that proved it is the one that used to stand at
// redisbroadcaster.go:87, `if r.IsGuardedChannel(channel)`, passing the raw
// name off the wire. A published channel is named "acme:private-orders.17",
// which does not begin with "private-" -- so the branch that protects private
// channels answered false for exactly the private channels it protects, and the
// subscription walked past the "nobody on the context" refusal underneath it.
func (UsePusherChannelConventions) IsGuardedChannel(channel string) bool {
	_, name, _ := broadcasting.CutTenant(channel)

	return strings.HasPrefix(name, broadcasting.PrivateChannelPrefix) ||
		strings.HasPrefix(name, broadcasting.PresenceChannelPrefix)
}

// NormalizeChannelName is UsePusherChannelConventions::normalizeChannelName:
// the channel name with its prefix taken off, which is the name channels are
// registered under.
//
// The tenant comes off here too, for the same reason and with a second one: the
// result is what Broadcaster.VerifyUserCanAccessChannel matches registered
// patterns against, and a name with the tenant still in it forces an
// application to register "{tenant}:private-orders.{orderId}" -- which makes
// Broadcaster.ExtractAuthParameters hand the handler a tenant taken out of the
// request. The pattern an application registers carries no tenant, because the
// name matched against it carries none.
//
// The order matters and is the PHP's: private-encrypted- is tried before
// private-, because it starts with it.
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
