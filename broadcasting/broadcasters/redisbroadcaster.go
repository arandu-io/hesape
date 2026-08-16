package broadcasters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
)

// RedisFactory is the little of a Redis connection factory that this
// broadcaster uses.
//
// It is declared here and not imported. github.com/arandu-io/hesape/redis is a
// separate module with its own go.mod, because the driver under it is a third
// party dependency and in Go there is no optional dependency; the root module
// cannot import its own submodule, and this package is in the root module. So
// the contract is stated here and an application wires its *redis.RedisManager
// through a two-line adapter -- Go has no covariant return types, so a manager
// whose Connection answers *connections.Connection does not satisfy an
// interface whose Connection answers an interface, however compatible the two
// are.
type RedisFactory interface {
	// Connection is the named connection, or the default one when the name is
	// empty.
	Connection(name string) (RedisConnection, error)
}

// RedisConnection is the one command [RedisBroadcaster.Broadcast] issues.
type RedisConnection interface {
	// Eval runs a Lua script server-side. numberOfKeys is how many of the
	// arguments are keys; the rest are ARGV.
	Eval(ctx context.Context, script string, numberOfKeys int, arguments ...any) (any, error)
}

// RedisBroadcaster publishes on Redis pub/sub, and a socket process on the
// other side relays to the browser.
//
// There is one publishing path: a Lua script that publishes to every channel in
// one round trip.
type RedisBroadcaster struct {
	Broadcaster
	UsePusherChannelConventions

	// redis is where a connection comes from.
	redis RedisFactory
	// connection is which Redis connection to publish through. Empty is the
	// default one.
	connection string
	// prefix is the key prefix put in front of every published channel, so the
	// channel a subscriber listens on and the key space the application writes
	// agree.
	prefix string
}

// NewRedisBroadcaster builds the driver over the factory it publishes through.
func NewRedisBroadcaster(redis RedisFactory, connection, prefix string) *RedisBroadcaster {
	return &RedisBroadcaster{redis: redis, connection: connection, prefix: prefix}
}

// Auth authorizes the incoming subscription.
//
// The channel name has the Redis prefix cut off the front -- as a prefix, not
// as the first occurrence anywhere in the name -- and is then normalized:
// private-, presence- and private-encrypted- come off, because that is the name
// channels are registered under. An empty channel is refused, and so is a
// guarded channel with nobody on the context.
//
// The decision is made by a Policy through auth.Authorize, the refusal is
// auth.ErrForbidden, and the auth.Grant that comes back is what the published
// channel name is built from. The subject comes from the context and never from
// the request being authorized.
//
// The name the client asked for goes through [broadcasting.RequestedChannel],
// which refuses a client that names a tenant, and the authorized name is built
// from the Grant by [RedisBroadcaster.ValidAuthenticationResponse] -- the same
// call [RedisBroadcaster.Broadcast] names its channels with.
func (r *RedisBroadcaster) Auth(ctx context.Context, channel string) (auth.Grant, any, error) {
	unprefixed, _ := strings.CutPrefix(channel, r.prefix)

	requested, err := broadcasting.RequestedChannel(unprefixed)
	if err != nil {
		return auth.Grant{}, nil, fmt.Errorf("%w: %w", auth.ErrForbidden, err)
	}

	name := r.NormalizeChannelName(requested.Name)

	if r.IsGuardedChannel(requested.Name) {
		if _, ok := r.RetrieveUser(ctx, name); !ok {
			return auth.Grant{}, nil, fmt.Errorf("%w: %s is guarded and nothing on the context says who is asking", auth.ErrForbidden, requested.Name)
		}
	}

	g, result, err := r.VerifyUserCanAccessChannel(ctx, name)
	if err != nil {
		return auth.Grant{}, nil, err
	}

	response, err := r.ValidAuthenticationResponse(ctx, g, requested, result)
	if err != nil {
		return auth.Grant{}, nil, err
	}

	return g, response, nil
}

// ValidAuthenticationResponse is the JSON document the socket client is sent
// back.
//
// A boolean result is the whole answer for a private channel. Anything else is
// the presence channel's user_info, wrapped in channel_data beside the
// identifier of whoever was authorized.
//
// That identifier is auth.Grant.Subject().ID: taking it off the Grant rather
// than off the request is what makes it the id that was authorized rather than
// the id that was claimed.
//
// The document names the channel it is about. An answer that said only `true`
// would leave the relay hearing yes without hearing yes-to-what, and it would
// sign the socket onto the string the client sent -- the only channel name it
// has. The name comes from [RedisBroadcaster.FormatChannels], which is what
// [RedisBroadcaster.Broadcast] names its channels with: one Grant, one name,
// both sides of the wire.
//
// It is also why this method cannot be called without a Grant that carries a
// tenant. A Grant that does not is refused here, and Auth answers the refusal.
func (r *RedisBroadcaster) ValidAuthenticationResponse(ctx context.Context, g auth.Grant, channel broadcasting.Channel, result any) (any, error) {
	names, err := r.FormatChannels(g, []broadcasting.Channel{channel})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", auth.ErrForbidden, err)
	}

	document := map[string]any{"channel": names[0]}
	if allowed, ok := result.(bool); ok {
		document["auth"] = allowed
	} else {
		document["channel_data"] = map[string]any{
			"user_id":   g.Subject().ID,
			"user_info": result,
		}
	}

	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, broadcasting.WrapBroadcastError(err, "broadcasting: encoding the authentication response: %v", err)
	}

	return string(encoded), nil
}

// Broadcast publishes the event on every channel in one round trip.
//
// The document is event, data and socket, with the socket id lifted out of the
// data. A subscriber that carries that socket id skips the message, and that is
// how ToOthers reaches the browser.
func (r *RedisBroadcaster) Broadcast(ctx context.Context, g auth.Grant, channels []broadcasting.Channel, event string, payload map[string]any) error {
	if len(channels) == 0 {
		return nil
	}

	names, err := r.FormatChannels(g, channels)
	if err != nil {
		return err
	}

	data := make(map[string]any, len(payload))
	for key, value := range payload {
		data[key] = value
	}
	socket := data["socket"]
	delete(data, "socket")

	document, err := json.Marshal(map[string]any{"event": event, "data": data, "socket": socket})
	if err != nil {
		return broadcasting.WrapBroadcastError(err, "broadcasting: encoding the payload of %s: %v", event, err)
	}

	if r.redis == nil {
		return broadcasting.NewBroadcastError("broadcasting: this RedisBroadcaster was built with no connection factory")
	}

	connection, err := r.redis.Connection(r.connection)
	if err != nil {
		return broadcasting.WrapBroadcastError(err, "broadcasting: redis error: %v", err)
	}

	arguments := make([]any, 0, len(names)+1)
	arguments = append(arguments, string(document))
	for _, name := range names {
		arguments = append(arguments, name)
	}

	if _, err := connection.Eval(ctx, r.broadcastMultipleChannelsScript(), 0, arguments...); err != nil {
		return broadcasting.WrapBroadcastError(err, "broadcasting: redis error: %v", err)
	}

	return nil
}

// FormatChannels is the channel names as they go on the wire, with the Redis
// key prefix in front.
//
// The name published is "<prefix><tenant>:<channel>", so two customers
// subscribing to the same channel name are on two channels, and neither of them
// chose the tenant -- it comes off the Grant that authorized the subscription.
//
// It shadows the embedded [Broadcaster.FormatChannels] rather than overriding
// it, because Go has no virtual dispatch; every call inside this driver reaches
// this one. What it adds is the Redis key prefix and nothing else -- the tenant
// is decided once, by the embedded method it calls, and not a second time here.
//
// Auth reaches this too, through
// [RedisBroadcaster.ValidAuthenticationResponse], so the name the authorization
// examines and the name Broadcast publishes are the same string built by the
// same call from the same Grant.
func (r *RedisBroadcaster) FormatChannels(g auth.Grant, channels []broadcasting.Channel) ([]string, error) {
	names, err := r.Broadcaster.FormatChannels(g, channels)
	if err != nil {
		return nil, err
	}

	prefixed := make([]string, 0, len(names))
	for _, name := range names {
		prefixed = append(prefixed, r.prefix+name)
	}

	return prefixed, nil
}

// broadcastMultipleChannelsScript publishes one payload on many channels.
//
// ARGV[1] is the payload and ARGV[2...] are the channels.
func (r *RedisBroadcaster) broadcastMultipleChannelsScript() string {
	return "for i = 2, #ARGV do\n  redis.call('publish', ARGV[i], ARGV[1])\nend"
}
