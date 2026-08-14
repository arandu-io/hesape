package broadcasting

import (
	"errors"
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/auth"
)

// ChannelJoin is the auth.Action every channel authorization is issued for.
//
// It has no counterpart in Illuminate, because Illuminate's channel callback is
// the decision. Here a decision is made by a Policy and produces an auth.Grant,
// and a Grant is issued for an action -- so listening on a channel is an action
// with a name, and auth.Grant.Check refuses a Grant issued for anything else.
//
// It lives here rather than beside the abstract broadcaster because
// [BroadcastController.Authenticate] has to check the Grant a driver answered,
// and this package cannot import the drivers -- they import it. It used to be
// declared only in broadcasters, and the controller could not name it, which is
// how `_, response, err := driver.Auth(...)` came to throw the Grant away.
const ChannelJoin auth.Action = "broadcasting.join"

// TenantSeparator is the ':' between the tenant and the channel in a published
// channel name, and the only character a channel name may not contain.
//
// It is named because both halves of [TenantChannel] and [CutTenant] spell it,
// and a separator spelled twice is a separator that drifts (RULE 9).
const TenantSeparator = ":"

// The prefixes Illuminate's three Channel subclasses put in front of a name.
//
// They are literals in PrivateChannel.php, PresenceChannel.php and
// EncryptedPrivateChannel.php, and again in UsePusherChannelConventions, which
// reads them back off an incoming channel name. They are named here so the
// writing side and the reading side cannot drift apart (RULE 9).
const (
	// PrivateChannelPrefix is the 'private-' of PrivateChannel::__construct.
	PrivateChannelPrefix = "private-"
	// PresenceChannelPrefix is the 'presence-' of PresenceChannel::__construct.
	PresenceChannelPrefix = "presence-"
	// EncryptedPrivateChannelPrefix is the 'private-encrypted-' of
	// EncryptedPrivateChannel::__construct.
	EncryptedPrivateChannelPrefix = "private-encrypted-"
)

// ErrNoTenant is returned when the Grant carries no tenant, or carries one that
// cannot be part of a channel name.
//
// It has no counterpart in Illuminate. It is RULE 14: a channel published
// without a tenant in its name is a channel every customer of the system can
// subscribe to.
var ErrNoTenant = errors.New("broadcasting: the Grant carries no tenant, and a channel without one belongs to every customer")

// ErrTenantInChannelName is returned when a channel name carries
// [TenantSeparator], which means somebody is naming a tenant.
//
// It has no counterpart in Illuminate either, and it is the other half of
// RULE 14. A tenant is added once, by [TenantChannel], out of the Grant. A name
// that already contains ':' either came from a client choosing whose events it
// hears, or from a publisher prefixing a second time; both are refused here
// rather than concatenated.
//
// Found by audit: with the tenant absent from the name the authentication
// endpoint examined, an application had to register the pattern
// "{tenant}:private-orders.{orderId}" for its channels to match at all -- and
// then Broadcaster.ExtractAuthParameters handed the handler a tenant taken out
// of the request, never compared to auth.Tenant on the Grant.
var ErrTenantInChannelName = errors.New("broadcasting: a channel name may not contain '" + TenantSeparator + "', which is where the tenant goes")

// HasBroadcastChannel is Illuminate\Contracts\Broadcasting\HasBroadcastChannel.
//
// It is the contract Channel::__construct accepts instead of a string: a model
// that knows the channel it is broadcast on, and the route pattern that channel
// is authorized under.
type HasBroadcastChannel interface {
	// BroadcastChannel is broadcastChannel(): the channel name for this
	// instance, e.g. "orders.17".
	BroadcastChannel() string
	// BroadcastChannelRoute is broadcastChannelRoute(): the pattern the channel
	// is registered under, e.g. "orders.{orderId}".
	BroadcastChannelRoute() string
}

// Channel is Illuminate\Broadcasting\Channel: the name an event goes out on.
//
// Illuminate's PrivateChannel, PresenceChannel and EncryptedPrivateChannel are
// subclasses whose entire body is a prefix handed to parent::__construct. A
// subclass that adds no behaviour is a constructor in Go, so they are
// [NewPrivateChannel], [NewPresenceChannel] and [NewEncryptedPrivateChannel],
// and all four produce this one type. Nothing in the broadcasters this
// ecosystem carries asks which subclass a channel came from -- they ask about
// the prefix, which is what UsePusherChannelConventions does in the PHP too.
type Channel struct {
	// Name is the channel's $name, the public property of Channel.php.
	Name string `json:"name"`
}

// NewChannel is Channel::__construct given a string.
func NewChannel(name string) Channel { return Channel{Name: name} }

// NewChannelFor is Channel::__construct given a HasBroadcastChannel, which is
// the `$name instanceof HasBroadcastChannel ? $name->broadcastChannel() : $name`
// half of the PHP constructor.
//
// It is a second constructor rather than a union parameter because Go has no
// union types; this is the third mechanical change of ADR 0044 read the only
// way it can be read here.
func NewChannelFor(h HasBroadcastChannel) Channel { return Channel{Name: h.BroadcastChannel()} }

// NewPrivateChannel is Illuminate\Broadcasting\PrivateChannel::__construct.
func NewPrivateChannel(name string) Channel { return Channel{Name: PrivateChannelPrefix + name} }

// NewPrivateChannelFor is PrivateChannel::__construct given a
// HasBroadcastChannel.
func NewPrivateChannelFor(h HasBroadcastChannel) Channel {
	return NewPrivateChannel(h.BroadcastChannel())
}

// NewPresenceChannel is Illuminate\Broadcasting\PresenceChannel::__construct.
func NewPresenceChannel(name string) Channel { return Channel{Name: PresenceChannelPrefix + name} }

// NewEncryptedPrivateChannel is
// Illuminate\Broadcasting\EncryptedPrivateChannel::__construct.
func NewEncryptedPrivateChannel(name string) Channel {
	return Channel{Name: EncryptedPrivateChannelPrefix + name}
}

// String is Channel::__toString.
//
// It is what Broadcaster::formatChannels calls when it casts a channel to a
// string on the way into a driver, and it makes a Channel a fmt.Stringer, which
// is the Go shape of PHP's Stringable.
func (c Channel) String() string { return c.Name }

// ToArray is Illuminate\Contracts\Support\Arrayable::toArray.
//
// Channel.php in this clone implements Stringable only, so this method answers
// no PHP method of the same name. It is here because a channel travels inside a
// broadcast payload, and json_encode of a PHP object emits its public
// properties -- one key, "name". Stating that is better than leaving the
// encoding to whichever driver serializes first.
func (c Channel) ToArray() map[string]any { return map[string]any{"name": c.Name} }

// JSONSerialize is JsonSerializable::jsonSerialize, and it answers ToArray, so
// the two encodings of a channel cannot disagree (RULE 9).
func (c Channel) JSONSerialize() any { return c.ToArray() }

// TenantChannel is the name a channel is actually published under:
// "<tenant>:<name>".
//
// It has no counterpart in Illuminate, and it is not optional here. RULE 14:
// the tenant comes from the Grant, never from the path, the body, the query or
// a header, and every key an application writes -- a cache key, a storage path,
// a scheduler lock -- is prefixed by it. A channel is the same kind of key with
// a subscriber on the other end: without the prefix, "private-orders.17" is one
// channel shared by every customer who has an order 17, and the first one to
// subscribe reads the others' events.
//
// Both sides of the wire go through this function. The publisher builds the
// name it publishes on, and the authentication endpoint answers about the same
// name, so a client that asks for "private-orders.17" is authorized for
// "acme:private-orders.17" and never gets to choose the "acme".
//
// That sentence was false when it was written, and this is what was wrong. The
// only callers of this function were RedisBroadcaster.Broadcast and
// LogBroadcaster.Broadcast -- the publishing side, both of them. The
// authenticating side was RedisBroadcaster.Auth, and the call that proved it is
// the one that used to stand at redisbroadcaster.go:85:
//
//	name := r.NormalizeChannelName(strings.Replace(channel, r.prefix, "", 1))
//
// It authorized the string the client sent, with no tenant anywhere in it. The
// publisher wrote "acme:private-orders.17" and the endpoint answered about
// "private-orders.17", so the two names never had to agree about whose channel
// it was. Now RedisBroadcaster.Auth reaches this function too, through
// [RedisBroadcaster.ValidAuthenticationResponse], and the name it answers is
// the name Broadcast writes (RULE 9).
//
// The zero Grant reaches no channel, which is the answer auth.Grant.Check gives
// for the same reason: a caller who authorized nothing has no tenant to build a
// name out of.
func TenantChannel(g auth.Grant, c Channel) (string, error) {
	tenant := auth.Tenant(g)
	if tenant == "" {
		return "", ErrNoTenant
	}
	// A Grant built by auth.Authorize carries whatever the session holds, so the
	// check belongs here as well as in auth.SystemGrant: this is the line that
	// turns a tenant into a namespace, and a tenant carrying ':' would land
	// inside somebody else's.
	if !auth.ValidTenant(tenant) {
		return "", fmt.Errorf("%w: %q cannot be part of a channel name", ErrNoTenant, tenant)
	}
	if c.Name == "" {
		return "", errors.New("broadcasting: an empty channel is not a channel")
	}
	// The channel half may not spell the separator either. Without this, a name
	// that already carried a tenant would be prefixed with a second one and
	// "acme:globex:orders.17" would be a channel two customers can construct.
	if strings.Contains(c.Name, TenantSeparator) {
		return "", fmt.Errorf("%w: %q", ErrTenantInChannelName, c.Name)
	}
	return tenant + TenantSeparator + c.Name, nil
}

// CutTenant is the inverse of [TenantChannel]: it splits the name that goes on
// the wire back into the tenant and the channel.
//
// found is false when there is no tenant in front, and then channel is the name
// unchanged -- which is the shape of every name a client sends, because a
// client never names a tenant.
//
// The left half has to be a tenant auth.ValidTenant accepts, so a channel that
// merely contains a colon is not read as one. [TenantChannel] refuses to build
// such a name, and this is the reading side of the same rule.
func CutTenant(name string) (tenant, channel string, found bool) {
	before, after, cut := strings.Cut(name, TenantSeparator)
	if !cut || !auth.ValidTenant(before) {
		return "", name, false
	}

	return before, after, true
}

// RequestedChannel is the intake for the one channel name in this package that
// arrives from outside: the "channel_name" field of the authentication request.
//
// It is a constructor and not a bare string so that the refusal happens once,
// at the edge, rather than in each driver's Auth. A client that names a tenant
// is refused rather than trusted or silently re-prefixed: a client that can put
// "acme:" in front of a channel is a client choosing whose events it hears,
// which is RULE 14 inverted.
//
// The tenant is added afterwards, by [TenantChannel], out of the Grant the
// Policy issued.
func RequestedChannel(name string) (Channel, error) {
	if name == "" {
		return Channel{}, errors.New("broadcasting: no channel was asked for")
	}
	if strings.Contains(name, TenantSeparator) {
		return Channel{}, fmt.Errorf("%w: %q was asked for, and the tenant comes from the Grant", ErrTenantInChannelName, name)
	}

	return Channel{Name: name}, nil
}

// TenantChannels is [TenantChannel] over a list, which is what every driver's
// Broadcast does first.
func TenantChannels(g auth.Grant, channels []Channel) ([]string, error) {
	names := make([]string, 0, len(channels))
	for _, c := range channels {
		name, err := TenantChannel(g, c)
		if err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}
