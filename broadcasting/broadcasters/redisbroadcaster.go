package broadcasters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
)

// RedisFactory is the little of Illuminate\Contracts\Redis\Factory that this
// broadcaster uses.
//
// It is declared here and not imported. github.com/arandu-io/hesape/redis is a
// separate module with its own go.mod, because the driver under it is a third
// party dependency and in Go there is no optional dependency (ADR 0048); the
// root module cannot import its own submodule, and this package is in the root
// module. So the contract is stated here and an application wires its
// *redis.RedisManager through a two-line adapter -- Go has no covariant return
// types, so a manager whose Connection answers *connections.Connection does not
// satisfy an interface whose Connection answers an interface, however
// compatible the two are.
type RedisFactory interface {
	// Connection is Factory::connection: the named connection, or the default
	// one when the name is empty.
	Connection(name string) (RedisConnection, error)
}

// RedisConnection is the one command RedisBroadcaster::broadcast issues.
type RedisConnection interface {
	// Eval is Connection::eval: run a Lua script server-side. numberOfKeys is
	// how many of the arguments are keys; the rest are ARGV.
	Eval(ctx context.Context, script string, numberOfKeys int, arguments ...any) (any, error)
}

// RedisBroadcaster is Illuminate\Broadcasting\Broadcasters\RedisBroadcaster: it
// publishes on Redis pub/sub, and something on the other side -- laravel-echo-server,
// soketi, a socket process of your own -- relays to the browser.
//
// The PHP has three code paths in broadcast(), one for a phpredis cluster, one
// for a predis cluster and one for everything else. They exist because those
// two clients disagree about how a script reaches a clustered server. There is
// one client here, so there is one path: the script, which publishes to every
// channel in one round trip.
type RedisBroadcaster struct {
	Broadcaster
	UsePusherChannelConventions

	// redis is the protected $redis.
	redis RedisFactory
	// connection is the protected $connection: which Redis connection to
	// publish through. Empty is the PHP's null, the default one.
	connection string
	// prefix is the protected $prefix: the database.redis.options.prefix the
	// manager passes in, so the channel a subscriber listens on and the key
	// space the application writes agree.
	prefix string
}

// NewRedisBroadcaster is RedisBroadcaster::__construct.
func NewRedisBroadcaster(redis RedisFactory, connection, prefix string) *RedisBroadcaster {
	return &RedisBroadcaster{redis: redis, connection: connection, prefix: prefix}
}

// Auth is RedisBroadcaster::auth: it authorizes the incoming subscription.
//
// The body is the PHP's, in the PHP's order. The channel name has the Redis
// prefix stripped and is then normalized -- private-, presence- and
// private-encrypted- come off, because that is the name channels are registered
// under. An empty channel is refused, and so is a guarded channel with nobody
// on the context, which is the PHP's `! $this->retrieveUser(...)`.
//
// What differs is the shape of the answer, and it is the point of this
// framework: the decision is made by a Policy through auth.Authorize, the
// refusal is auth.ErrForbidden, and the auth.Grant that comes back is what the
// published channel name is built from (RULE 14). The subject comes from the
// context and never from the request being authorized.
func (r *RedisBroadcaster) Auth(ctx context.Context, channel string) (auth.Grant, any, error) {
	if channel == "" {
		return auth.Grant{}, nil, fmt.Errorf("%w: no channel was asked for", auth.ErrForbidden)
	}

	name := r.NormalizeChannelName(strings.Replace(channel, r.prefix, "", 1))

	if r.IsGuardedChannel(channel) {
		if _, ok := r.RetrieveUser(ctx, name); !ok {
			return auth.Grant{}, nil, fmt.Errorf("%w: %s is guarded and nothing on the context says who is asking", auth.ErrForbidden, channel)
		}
	}

	g, result, err := r.VerifyUserCanAccessChannel(ctx, name)
	if err != nil {
		return auth.Grant{}, nil, err
	}

	response, err := r.ValidAuthenticationResponse(ctx, g, result)
	if err != nil {
		return auth.Grant{}, nil, err
	}

	return g, response, nil
}

// ValidAuthenticationResponse is RedisBroadcaster::validAuthenticationResponse:
// the JSON document the socket client is sent back.
//
// A boolean result is encoded on its own, which is the whole answer for a
// private channel. Anything else is the presence channel's user_info, wrapped
// in channel_data beside the identifier of whoever was authorized.
//
// That identifier is auth.Grant.Subject().ID. The PHP asks the user model for
// getAuthIdentifierForBroadcasting() and falls back to getAuthIdentifier();
// there is one identifier on an auth.Subject, and taking it off the Grant
// rather than off the request is what makes it the id that was authorized
// rather than the id that was claimed.
func (r *RedisBroadcaster) ValidAuthenticationResponse(ctx context.Context, g auth.Grant, result any) (any, error) {
	if allowed, ok := result.(bool); ok {
		encoded, err := json.Marshal(allowed)
		if err != nil {
			return nil, broadcasting.WrapBroadcastError(err, "broadcasting: encoding the authentication response: %v", err)
		}

		return string(encoded), nil
	}

	encoded, err := json.Marshal(map[string]any{
		"channel_data": map[string]any{
			"user_id":   g.Subject().ID,
			"user_info": result,
		},
	})
	if err != nil {
		return nil, broadcasting.WrapBroadcastError(err, "broadcasting: encoding the authentication response: %v", err)
	}

	return string(encoded), nil
}

// Broadcast is RedisBroadcaster::broadcast: publish the event on every channel
// in one round trip.
//
// The payload is the PHP's document -- event, data, socket -- with the socket id
// pulled out of the data, which is what Arr::pull does. A subscriber that
// carries that socket id skips the message, and that is how toOthers reaches
// the browser.
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

// FormatChannels is RedisBroadcaster::formatChannels: the channel names as they
// go on the wire, with the Redis key prefix in front.
//
// The Grant is the addition, and it is RULE 14. The name published is
// "<prefix><tenant>:<channel>", so two customers subscribing to the same
// channel name are on two channels, and neither of them chose the tenant --
// it comes off the Grant that authorized the subscription.
//
// It shadows the embedded [Broadcaster.FormatChannels] rather than overriding
// it, because Go has no virtual dispatch; every call inside this driver reaches
// this one.
func (r *RedisBroadcaster) FormatChannels(g auth.Grant, channels []broadcasting.Channel) ([]string, error) {
	names, err := broadcasting.TenantChannels(g, channels)
	if err != nil {
		return nil, err
	}

	prefixed := make([]string, 0, len(names))
	for _, name := range names {
		prefixed = append(prefixed, r.prefix+name)
	}

	return prefixed, nil
}

// broadcastMultipleChannelsScript is
// RedisBroadcaster::broadcastMultipleChannelsScript, verbatim.
//
// ARGV[1] is the payload and ARGV[2...] are the channels.
func (r *RedisBroadcaster) broadcastMultipleChannelsScript() string {
	return "for i = 2, #ARGV do\n  redis.call('publish', ARGV[i], ARGV[1])\nend"
}
