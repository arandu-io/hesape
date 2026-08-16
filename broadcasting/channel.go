package broadcasting

import (
	"errors"
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/auth"
)

// ChannelJoin is the auth.Action every channel authorization is issued for.
//
// A decision is made by a Policy and produces an auth.Grant, and a Grant is
// issued for an action -- so listening on a channel is an action with a name,
// and auth.Grant.Check refuses a Grant issued for anything else.
//
// It lives here rather than beside the abstract broadcaster because
// [BroadcastController.Authenticate] has to check the Grant a driver answered,
// and this package cannot import the drivers -- they import it.
const ChannelJoin auth.Action = "broadcasting.join"

// TenantSeparator is the ':' between the tenant and the channel in a published
// channel name, and the only character a channel name may not contain.
//
// It is named because both halves of [TenantChannel] and [CutTenant] spell it,
// and a separator spelled twice is a separator that drifts.
const TenantSeparator = ":"

// The prefixes the three kinds of guarded channel carry in front of a name.
//
// They are written by the constructors below and read back off an incoming
// channel name by UsePusherChannelConventions. They are named here so the
// writing side and the reading side cannot drift apart.
const (
	// PrivateChannelPrefix marks a channel that has to be authorized.
	PrivateChannelPrefix = "private-"
	// PresenceChannelPrefix marks a channel that has to be authorized and
	// reports who is listening.
	PresenceChannelPrefix = "presence-"
	// EncryptedPrivateChannelPrefix marks a private channel whose payloads are
	// encrypted end to end.
	EncryptedPrivateChannelPrefix = "private-encrypted-"
)

// ErrNoTenant is returned when the Grant carries no tenant, or carries one that
// cannot be part of a channel name.
//
// A channel published without a tenant in its name is a channel every customer
// of the system can subscribe to.
var ErrNoTenant = errors.New("broadcasting: the Grant carries no tenant, and a channel without one belongs to every customer")

// ErrTenantInChannelName is returned when a channel name carries
// [TenantSeparator], which means somebody is naming a tenant.
//
// A tenant is added once, by [TenantChannel], out of the Grant. A name that
// already contains ':' either came from a client choosing whose events it
// hears, or from a publisher prefixing a second time; both are refused here
// rather than concatenated.
//
// Without the refusal, an application has to register the pattern
// "{tenant}:private-orders.{orderId}" for its channels to match at all, and
// Broadcaster.ExtractAuthParameters then hands the handler a tenant taken out
// of the request rather than off the Grant.
var ErrTenantInChannelName = errors.New("broadcasting: a channel name may not contain '" + TenantSeparator + "', which is where the tenant goes")

// HasBroadcastChannel is a model that knows the channel it is broadcast on, and
// the pattern that channel is authorized under.
//
// [NewChannelFor] and the other For constructors accept one in place of a name.
type HasBroadcastChannel interface {
	// BroadcastChannel is the channel name for this instance, e.g. "orders.17".
	BroadcastChannel() string
	// BroadcastChannelRoute is the pattern the channel is registered under,
	// e.g. "orders.{orderId}".
	BroadcastChannelRoute() string
}

// Channel is the name an event goes out on.
//
// A private, presence or encrypted channel is this same type with a prefix in
// front of the name, put there by [NewPrivateChannel], [NewPresenceChannel] or
// [NewEncryptedPrivateChannel]. Nothing asks which constructor built a channel;
// the drivers ask about the prefix.
type Channel struct {
	// Name is the channel name, prefix included.
	Name string `json:"name"`
}

// NewChannel names a channel.
func NewChannel(name string) Channel { return Channel{Name: name} }

// NewChannelFor names the channel a model is broadcast on.
//
// It is a second constructor rather than one parameter accepting either shape,
// because Go has no union types.
func NewChannelFor(h HasBroadcastChannel) Channel { return Channel{Name: h.BroadcastChannel()} }

// NewPrivateChannel names a channel that has to be authorized.
func NewPrivateChannel(name string) Channel { return Channel{Name: PrivateChannelPrefix + name} }

// NewPrivateChannelFor names the private channel a model is broadcast on.
func NewPrivateChannelFor(h HasBroadcastChannel) Channel {
	return NewPrivateChannel(h.BroadcastChannel())
}

// NewPresenceChannel names a channel that has to be authorized and reports who
// is listening.
func NewPresenceChannel(name string) Channel { return Channel{Name: PresenceChannelPrefix + name} }

// NewEncryptedPrivateChannel names a private channel whose payloads are
// encrypted end to end, with the "private-encrypted-" prefix that says so.
//
// The prefix is not decoration: it is how the client knows to decrypt, and how
// the broadcaster knows to encrypt. A channel that carries the prefix and is not
// encrypted, or the reverse, fails at the far end with nothing useful said.
func NewEncryptedPrivateChannel(name string) Channel {
	return Channel{Name: EncryptedPrivateChannelPrefix + name}
}

// String is the channel name, and it makes a Channel a fmt.Stringer.
func (c Channel) String() string { return c.Name }

// ToArray is the channel as it travels inside a broadcast payload: one key,
// "name".
//
// It is stated here rather than left to whichever driver serializes first.
func (c Channel) ToArray() map[string]any { return map[string]any{"name": c.Name} }

// JSONSerialize answers ToArray, so the two encodings of a channel cannot
// disagree.
func (c Channel) JSONSerialize() any { return c.ToArray() }

// TenantChannel is the name a channel is actually published under:
// "<tenant>:<name>".
//
// The tenant comes from the Grant, never from the path, the body, the query or
// a header, and every key an application writes -- a cache key, a storage path,
// a scheduler lock -- is prefixed by it. A channel is the same kind of key with
// a subscriber on the other end: without the prefix, "private-orders.17" is one
// channel shared by every customer who has an order 17, and the first one to
// subscribe reads the others' events.
//
// Both sides of the wire go through this function: the publisher builds the
// name it publishes on, and the authentication endpoint answers about the same
// name, so a client that asks for "private-orders.17" is authorized for
// "acme:private-orders.17" and never gets to choose the "acme".
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
// "acme:" in front of a channel is a client choosing whose events it hears.
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
