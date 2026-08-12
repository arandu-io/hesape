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
// even though Pusher itself does not (reason (3) of ADR 0044): the conventions
// are the wire format of the whole Laravel Echo client, and RedisBroadcaster
// uses the trait in the PHP too.
type UsePusherChannelConventions struct{}

// IsGuardedChannel is UsePusherChannelConventions::isGuardedChannel: true when
// the channel is one that has to be authorized.
func (UsePusherChannelConventions) IsGuardedChannel(channel string) bool {
	return strings.HasPrefix(channel, broadcasting.PrivateChannelPrefix) ||
		strings.HasPrefix(channel, broadcasting.PresenceChannelPrefix)
}

// NormalizeChannelName is UsePusherChannelConventions::normalizeChannelName:
// the channel name with its prefix taken off, which is the name channels are
// registered under.
//
// The order matters and is the PHP's: private-encrypted- is tried before
// private-, because it starts with it.
func (UsePusherChannelConventions) NormalizeChannelName(channel string) string {
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
