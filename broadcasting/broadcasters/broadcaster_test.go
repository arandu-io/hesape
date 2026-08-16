package broadcasters_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
	"github.com/arandu-io/hesape/broadcasting/broadcasters"
)

// recordingConnection is the RedisConnection a test publishes through: it keeps
// the arguments the Lua script was called with, which are the payload followed
// by the channel names as they go on the wire.
type recordingConnection struct{ arguments []any }

func (c *recordingConnection) Eval(ctx context.Context, script string, numberOfKeys int, arguments ...any) (any, error) {
	c.arguments = append(c.arguments, arguments...)

	return nil, nil
}

type recordingFactory struct{ connection *recordingConnection }

func (f *recordingFactory) Connection(name string) (broadcasters.RedisConnection, error) {
	return f.connection, nil
}

// newRedisBroadcaster builds a driver with one registered channel and the
// connection it publishes through.
func newRedisBroadcaster(t *testing.T, prefix, pattern string) (*broadcasters.RedisBroadcaster, *recordingConnection) {
	t.Helper()

	connection := &recordingConnection{}
	broadcaster := broadcasters.NewRedisBroadcaster(&recordingFactory{connection: connection}, "", prefix)
	broadcaster.Channel(pattern, broadcasters.ChannelHandlerFunc(
		func(ctx context.Context, s auth.Subject, parameters map[string]string) (any, error) {
			return true, nil
		},
	))

	return broadcaster, connection
}

// asking is a request from a signed-in subject of the given tenant. The subject
// is on the context, where the session middleware puts it, and never in the
// request being authorized.
func asking(tenant string) context.Context {
	return auth.WithSubject(context.Background(), auth.Subject{ID: "u-1", Tenant: tenant})
}

// authorizedChannel reads the channel name out of the document Auth answered.
// It is the name the relay signs the socket onto.
func authorizedChannel(t *testing.T, response any) string {
	t.Helper()

	document, ok := response.(string)
	if !ok {
		t.Fatalf("the authentication response is %T, want the JSON document", response)
	}

	var body struct {
		Channel string `json:"channel"`
		Auth    bool   `json:"auth"`
	}
	if err := json.Unmarshal([]byte(document), &body); err != nil {
		t.Fatalf("the authentication response %q is not a document: %v", document, err)
	}
	if body.Channel == "" {
		t.Fatalf("the authentication response %q says yes without saying yes to what", document)
	}

	return body.Channel
}

// publishedChannel is the first channel name Broadcast handed to Redis. The
// payload is ARGV[1], so the channels start at index one.
func publishedChannel(t *testing.T, connection *recordingConnection) string {
	t.Helper()

	if len(connection.arguments) < 2 {
		t.Fatalf("nothing was published: %v", connection.arguments)
	}
	name, ok := connection.arguments[1].(string)
	if !ok {
		t.Fatalf("the published channel is %T, want a string", connection.arguments[1])
	}

	return name
}

// The fix, stated as one assertion: the name the authorization examines and the
// name the publication writes are the same string, for the same Grant.
//
// They were not. Auth answered about the raw name the client sent and Broadcast
// published "<prefix><tenant>:<channel>", so the two sides never had to agree
// about whose channel it was.
func TestTheAuthorizedNameIsTheNameThePublisherWrites(t *testing.T) {
	broadcaster, connection := newRedisBroadcaster(t, "hesape:", "orders.{orderId}")
	ctx := asking("acme")

	g, response, err := broadcaster.Auth(ctx, "private-orders.17")
	if err != nil {
		t.Fatalf("an authorized subscription was refused: %v", err)
	}

	channels := []broadcasting.Channel{broadcasting.NewPrivateChannel("orders.17")}
	if err := broadcaster.Broadcast(ctx, g, channels, "OrderShipped", map[string]any{"id": 17}); err != nil {
		t.Fatalf("the event was not published: %v", err)
	}

	authorized, published := authorizedChannel(t, response), publishedChannel(t, connection)

	if authorized != published {
		t.Fatalf("the subscription was authorized for %q and the event went out on %q", authorized, published)
	}
	if published != "hesape:acme:private-orders.17" {
		t.Errorf("the channel on the wire is %q, want the prefix, then the tenant, then the channel", published)
	}
}

// The pattern an application registers carries no tenant, because the name
// matched against it carries none. A pattern that names one matches nothing --
// which is what stops Broadcaster.ExtractAuthParameters from ever handing a
// handler a tenant taken out of the request.
func TestAPatternDoesNotNameTheTenant(t *testing.T) {
	broadcaster, _ := newRedisBroadcaster(t, "", "{tenant}:orders.{orderId}")

	if _, _, err := broadcaster.Auth(asking("acme"), "private-orders.17"); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("a pattern naming the tenant matched, error was %v", err)
	}
}

// One tenant is not authorizable for another's channel, and sending the exact
// name the other's events go out on does not change that.
func TestOneTenantIsNotAuthorizableForAnothersChannel(t *testing.T) {
	broadcaster, _ := newRedisBroadcaster(t, "", "orders.{orderId}")

	_, acme, err := broadcaster.Auth(asking("acme"), "private-orders.17")
	if err != nil {
		t.Fatalf("acme was refused its own channel: %v", err)
	}
	_, globex, err := broadcaster.Auth(asking("globex"), "private-orders.17")
	if err != nil {
		t.Fatalf("globex was refused its own channel: %v", err)
	}

	acmeChannel, globexChannel := authorizedChannel(t, acme), authorizedChannel(t, globex)
	if acmeChannel == globexChannel {
		t.Fatalf("both tenants were authorized for %q, so either one hears the other's events", acmeChannel)
	}

	// The same request, with the name spelled exactly as acme's events go out
	// on it. It is refused: the tenant is not the client's to send.
	if _, _, err := broadcaster.Auth(asking("globex"), acmeChannel); !errors.Is(err, broadcasting.ErrTenantInChannelName) {
		t.Errorf("globex asked for %q and the error was %v, want a refusal to read a tenant off the request", acmeChannel, err)
	}
	if _, _, err := broadcaster.Auth(asking("globex"), acmeChannel); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("the refusal of %q is not auth.ErrForbidden, so a handler will not render it 403", acmeChannel)
	}

	// And acme cannot re-send its own wire name either, which is the point:
	// nothing on that endpoint reads a tenant off the request.
	if _, _, err := broadcaster.Auth(asking("acme"), acmeChannel); err == nil {
		t.Errorf("%q was accepted from the client that owns it, and the endpoint read the tenant off the request", acmeChannel)
	}
}

// A private channel is private after the tenant prefix.
//
// IsGuardedChannel used to be asked about the raw wire name, and
// "acme:private-orders.17" does not begin with "private-" -- so the branch that
// refuses a guarded channel with nobody on the context answered false for
// exactly the channels it guards.
func TestAPrivateChannelIsPrivateAfterTheTenantPrefix(t *testing.T) {
	conventions := broadcasters.UsePusherChannelConventions{}

	for _, wire := range []string{
		"acme:" + broadcasting.PrivateChannelPrefix + "orders.17",
		"acme:" + broadcasting.PresenceChannelPrefix + "orders.17",
		"acme:" + broadcasting.EncryptedPrivateChannelPrefix + "orders.17",
	} {
		// The naive check is what was there, and this states its answer: the
		// tenant is in front, so the guard prefix is not.
		if strings.HasPrefix(wire, broadcasting.PrivateChannelPrefix) {
			t.Fatalf("%q begins with the private prefix, so this test proves nothing", wire)
		}
		if !conventions.IsGuardedChannel(wire) {
			t.Errorf("%q was not recognized as guarded", wire)
		}
		if name := conventions.NormalizeChannelName(wire); name != "orders.17" {
			t.Errorf("%q normalizes to %q, want orders.17 -- the name channels are registered under", wire, name)
		}
	}

	if conventions.IsGuardedChannel("acme:orders.17") {
		t.Error("a public channel was recognized as guarded")
	}
	if name := conventions.NormalizeChannelName("acme:orders.17"); name != "orders.17" {
		t.Errorf("a public channel normalizes to %q, want orders.17", name)
	}
}

// The guarded branch refuses a subscription with nobody on the context, and it
// is reached now that the name it tests is the client's, with no tenant in
// front.
func TestAGuardedChannelWithNobodyOnTheContextIsRefused(t *testing.T) {
	broadcaster, _ := newRedisBroadcaster(t, "", "orders.{orderId}")

	_, _, err := broadcaster.Auth(context.Background(), "private-orders.17")
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a subscription with no subject was not refused, error was %v", err)
	}
	if !strings.Contains(err.Error(), "guarded") {
		t.Errorf("the refusal is %q, want the guarded-channel branch to be the one that answered", err)
	}
}

// A Grant with no tenant authorizes no channel. The Policy allowed; what
// refuses is that there is no tenant to name the channel with.
func TestAGrantWithNoTenantAuthorizesNoChannel(t *testing.T) {
	broadcaster, connection := newRedisBroadcaster(t, "", "orders.{orderId}")
	ctx := auth.WithSubject(context.Background(), auth.Subject{ID: "u-1"})

	_, _, err := broadcaster.Auth(ctx, "private-orders.17")
	if !errors.Is(err, broadcasting.ErrNoTenant) {
		t.Fatalf("a subject with no tenant was authorized, error was %v", err)
	}
	if !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("the refusal is %v, want auth.ErrForbidden so a handler renders it 403", err)
	}

	// The publishing side answers the same, and publishes nothing.
	g, err := auth.Authorize(ctx, allowAllChannels{}, auth.Subject{ID: "u-1"}, broadcasting.ChannelJoin, broadcasters.ChannelAuthorization{})
	if err != nil {
		t.Fatalf("the policy refused: %v", err)
	}
	channels := []broadcasting.Channel{broadcasting.NewPrivateChannel("orders.17")}
	if err := broadcaster.Broadcast(context.Background(), g, channels, "OrderShipped", nil); !errors.Is(err, broadcasting.ErrNoTenant) {
		t.Errorf("an event was published without a tenant, error was %v", err)
	}
	if len(connection.arguments) != 0 {
		t.Errorf("something reached Redis without a tenant: %v", connection.arguments)
	}
}

// allowAllChannels is a Policy that permits any channel, so a test can hold a
// valid Grant that carries no tenant.
type allowAllChannels struct{}

func (allowAllChannels) Can(ctx context.Context, s auth.Subject, a auth.Action, resource broadcasters.ChannelAuthorization) error {
	return nil
}

// The Redis key prefix is removed as a prefix, not as the first occurrence
// anywhere in the name.
//
// strings.Replace(channel, "ac", "", 1) turned "private-acme.17" into
// "private-me.17" -- the "ac" it found is the one inside "acme", at index 8 --
// and the subscription was then authorized against a channel nobody registered.
func TestTheRedisPrefixIsRemovedAsAPrefix(t *testing.T) {
	broadcaster, _ := newRedisBroadcaster(t, "ac", "acme.{orderId}")

	if index := strings.Index("private-acme.17", "ac"); index != 8 {
		t.Fatalf("the prefix occurs at %d, so this test no longer proves anything", index)
	}

	_, response, err := broadcaster.Auth(asking("acme"), "private-acme.17")
	if err != nil {
		t.Fatalf("private-acme.17 was refused, which is what removing the first \"ac\" anywhere did: %v", err)
	}
	if channel := authorizedChannel(t, response); channel != "acacme:private-acme.17" {
		t.Errorf("the authorized channel is %q, want acacme:private-acme.17", channel)
	}

	// The prefixed spelling is still stripped, once, off the front, and lands
	// on the same channel.
	_, response, err = broadcaster.Auth(asking("acme"), "acprivate-acme.17")
	if err != nil {
		t.Fatalf("the prefixed spelling was refused: %v", err)
	}
	if channel := authorizedChannel(t, response); channel != "acacme:private-acme.17" {
		t.Errorf("the prefixed spelling authorized %q, want the same channel as the bare one", channel)
	}
}

// A presence channel answers channel_data, and the id in it is the one that was
// authorized -- off the Grant, not off the request -- beside the channel the
// answer is about.
func TestAPresenceChannelAnswersTheSubjectThatWasAuthorized(t *testing.T) {
	connection := &recordingConnection{}
	broadcaster := broadcasters.NewRedisBroadcaster(&recordingFactory{connection: connection}, "", "")
	broadcaster.Channel("rooms.{roomId}", broadcasters.ChannelHandlerFunc(
		func(ctx context.Context, s auth.Subject, parameters map[string]string) (any, error) {
			return map[string]any{"name": "Ada"}, nil
		},
	))

	_, response, err := broadcaster.Auth(asking("acme"), "presence-rooms.5")
	if err != nil {
		t.Fatalf("a presence subscription was refused: %v", err)
	}

	document, _ := response.(string)

	var body struct {
		Channel     string `json:"channel"`
		ChannelData struct {
			UserID   string         `json:"user_id"`
			UserInfo map[string]any `json:"user_info"`
		} `json:"channel_data"`
	}
	if err := json.Unmarshal([]byte(document), &body); err != nil {
		t.Fatalf("the response %q is not a document: %v", document, err)
	}

	if body.Channel != "acme:presence-rooms.5" {
		t.Errorf("the presence answer is about %q, want acme:presence-rooms.5", body.Channel)
	}
	if body.ChannelData.UserID != "u-1" {
		t.Errorf("the answer names user %q, want the subject the Grant was issued for", body.ChannelData.UserID)
	}
	if body.ChannelData.UserInfo["name"] != "Ada" {
		t.Errorf("the presence payload is %v, want the handler's", body.ChannelData.UserInfo)
	}
}

// The second finding: the promoted FormatChannels dropped the tenant, had no
// caller and no test, and only RedisBroadcaster shadowed it. LogBroadcaster and
// NullBroadcaster inherit it, so the next driver written by copying one of them
// would have inherited a tenant-less name formatter in silence.
//
// It takes the Grant now, so the tenant-less call does not compile, and every
// driver that does not shadow it gets the tenant.
func TestEveryDriverInheritsANameWithTheTenant(t *testing.T) {
	channels := []broadcasting.Channel{broadcasting.NewPrivateChannel("orders.17")}

	log := broadcasters.NewLogBroadcaster(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	names, err := log.FormatChannels(auth.SystemGrant(broadcasting.ChannelJoin, "acme"), channels)
	if err != nil {
		t.Fatalf("the inherited formatter refused an ordinary channel: %v", err)
	}
	if len(names) != 1 || names[0] != "acme:private-orders.17" {
		t.Errorf("the inherited formatter answered %v, want [acme:private-orders.17]", names)
	}

	if _, err := log.FormatChannels(auth.Grant{}, channels); !errors.Is(err, broadcasting.ErrNoTenant) {
		t.Errorf("the inherited formatter named a channel without a tenant, error was %v", err)
	}

	if _, err := broadcasters.NewNullBroadcaster().FormatChannels(auth.Grant{}, channels); !errors.Is(err, broadcasting.ErrNoTenant) {
		t.Errorf("NullBroadcaster named a channel without a tenant, error was %v", err)
	}
}

// The log line is the only evidence a broadcast happened, so it carries the
// name the broker would have seen -- tenant included.
func TestTheLogBroadcasterWritesTheTenantQualifiedName(t *testing.T) {
	var written bytes.Buffer

	log := broadcasters.NewLogBroadcaster(slog.New(slog.NewTextHandler(&written, nil)))
	channels := []broadcasting.Channel{broadcasting.NewPrivateChannel("orders.17")}

	if err := log.Broadcast(context.Background(), auth.SystemGrant(broadcasting.ChannelJoin, "acme"), channels, "OrderShipped", map[string]any{"id": 17}); err != nil {
		t.Fatalf("the log broadcaster refused: %v", err)
	}
	if !strings.Contains(written.String(), "acme:private-orders.17") {
		t.Errorf("the log says %q, want the channel the broker would have seen", written.String())
	}

	written.Reset()
	if err := log.Broadcast(context.Background(), auth.Grant{}, channels, "OrderShipped", nil); !errors.Is(err, broadcasting.ErrNoTenant) {
		t.Errorf("the log broadcaster wrote a channel with no tenant, error was %v", err)
	}
}

// A driver whose whole body is a comment authorizes nobody, and says so with a
// Grant that fails Check -- which is what BroadcastController reads.
func TestADriverThatDecidesNothingAnswersAGrantThatFailsCheck(t *testing.T) {
	drivers := map[string]broadcasting.Broadcaster{
		"log":  broadcasters.NewLogBroadcaster(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))),
		"null": broadcasters.NewNullBroadcaster(),
	}

	for name, driver := range drivers {
		g, _, err := driver.Auth(asking("acme"), "private-orders.17")
		if err != nil {
			t.Fatalf("%s answered an error, and the controller reads the Grant: %v", name, err)
		}
		if err := g.Check(broadcasting.ChannelJoin); err == nil {
			t.Errorf("%s authorized a private channel without deciding anything", name)
		}
	}
}

// ChannelNameMatchesPattern answers about normalized names, which carry no
// tenant on either side.
func TestChannelNameMatchesPattern(t *testing.T) {
	broadcaster, _ := newRedisBroadcaster(t, "", "orders.{orderId}")

	if !broadcaster.ChannelNameMatchesPattern("orders.17", "orders.{orderId}") {
		t.Error("orders.17 does not match the pattern it was registered under")
	}
	if broadcaster.ChannelNameMatchesPattern("orders.17.lines", "orders.{orderId}") {
		t.Error("a placeholder matched across a dot, so a deeper channel matches a shallower pattern")
	}
	if broadcaster.ChannelNameMatchesPattern("acme:orders.17", "orders.{orderId}") {
		t.Error("a name with a tenant in front matched a pattern without one")
	}
}

// The parameters a handler is given come out of the channel, and none of them
// is a tenant.
func TestTheParametersAHandlerIsGivenCarryNoTenant(t *testing.T) {
	connection := &recordingConnection{}
	broadcaster := broadcasters.NewRedisBroadcaster(&recordingFactory{connection: connection}, "", "")

	var seen map[string]string
	broadcaster.Channel("orders.{orderId}", broadcasters.ChannelHandlerFunc(
		func(ctx context.Context, s auth.Subject, parameters map[string]string) (any, error) {
			seen = parameters

			return true, nil
		},
	))

	if _, _, err := broadcaster.Auth(asking("acme"), "private-orders.17"); err != nil {
		t.Fatalf("the subscription was refused: %v", err)
	}

	if seen["orderId"] != "17" {
		t.Errorf("the handler was given %v, want orderId 17", seen)
	}
	if _, ok := seen["tenant"]; ok {
		t.Errorf("the handler was given a tenant out of the request: %v", seen)
	}
}
