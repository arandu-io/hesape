package broadcasters

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
)

// ChannelJoin is [broadcasting.ChannelJoin], which is where the action is
// declared so that BroadcastController can check the Grant a driver answered.
//
// It stays spelled here because this is the package that issues the Grant, and
// it is the same constant rather than a second one: two spellings of an action
// is a Grant that passes Check in one package and fails it in the other.
const ChannelJoin = broadcasting.ChannelJoin

// ErrChannelUndecided is what a channel handler returns to say the pattern it
// was registered under does not apply after all, so the search should carry on
// to the next one.
//
// A handler has three answers: false denies immediately, any other value
// allows, and (nil, nil) keeps looking. This error is what the third turns into
// once auth.Authorize has been asked -- a Policy that returns nil has allowed,
// so "keep looking" has to be an error to travel back out.
var ErrChannelUndecided = errors.New("broadcasting: the channel handler did not decide")

// ChannelAuthorization is the resource a channel Policy decides about.
//
// The pattern's parameters arrive as a map rather than spread across the
// handler's arguments, because Go cannot inspect a func's parameter names: the
// handler reads the ones it registered for.
type ChannelAuthorization struct {
	// Name is the normalized channel name -- no private-, presence- or
	// private-encrypted- prefix, and no Redis key prefix.
	Name string
	// Parameters are the {placeholders} of the registered pattern, filled in
	// from the channel the client asked for: "orders.{orderId}" against
	// "orders.17" gives {"orderId": "17"}.
	Parameters map[string]string
}

// ChannelHandler is what [Broadcaster.Channel] registers: the thing that
// decides whether a subject may listen on a channel.
//
// [ChannelHandlerFunc] is the plain-function form of the same thing.
type ChannelHandler interface {
	// Join decides whether the subject may listen on the channel.
	//
	// A nil error allows. A non-nil error denies. A nil result with a nil error
	// means this pattern declines to decide and the next one is tried -- see
	// [ErrChannelUndecided].
	//
	// The value returned on success is what ValidAuthenticationResponse turns
	// into the presence channel's user_info. `true` is the ordinary answer for
	// a private channel.
	Join(ctx context.Context, s auth.Subject, parameters map[string]string) (any, error)
}

// ChannelHandlerFunc is a function that is a [ChannelHandler].
type ChannelHandlerFunc func(ctx context.Context, s auth.Subject, parameters map[string]string) (any, error)

// Join calls f, so a plain function satisfies [ChannelHandler].
func (f ChannelHandlerFunc) Join(ctx context.Context, s auth.Subject, parameters map[string]string) (any, error) {
	return f(ctx, s, parameters)
}

// Broadcaster is the channel registry and the authorization walk every driver
// shares, and it is embedded by each of them.
//
// Embedding cannot call back into the driver -- Go has no virtual dispatch --
// so [Broadcaster.VerifyUserCanAccessChannel] answers the raw handler result
// instead of a response, and each driver's Auth passes that through its own
// ValidAuthenticationResponse.
//
// A Broadcaster is safe for concurrent use: channels are registered at start-up
// and read on every subscription.
type Broadcaster struct {
	mu sync.RWMutex
	// authenticatedUserCallback answers who a connection belongs to.
	authenticatedUserCallback func(ctx context.Context, r *http.Request) (any, error)
	// channels is the registered handler for each pattern.
	channels map[string]ChannelHandler
	// patterns caches the compiled form of each registered pattern.
	patterns map[string]*regexp.Regexp
	// order is the registration order, which a Go map does not keep: it decides
	// which of two overlapping patterns answers first.
	order []string
}

// Channel registers a channel authenticator under a pattern.
//
// There is no options argument: the subject comes from the context
// (auth.SubjectFrom), so there is no authentication guard to pick between.
//
// Registering the same pattern twice replaces the handler and keeps its place
// in the registration order.
func (b *Broadcaster) Channel(channel string, handler ChannelHandler) *Broadcaster {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.channels == nil {
		b.channels = map[string]ChannelHandler{}
		b.patterns = map[string]*regexp.Regexp{}
	}
	if _, seen := b.channels[channel]; !seen {
		b.order = append(b.order, channel)
	}
	b.channels[channel] = handler
	b.patterns[channel] = compileChannelPattern(channel)

	return b
}

// ChannelFor registers a channel authenticator for a model, under its
// BroadcastChannelRoute -- the pattern, not the instance's own name.
func (b *Broadcaster) ChannelFor(channel broadcasting.HasBroadcastChannel, handler ChannelHandler) *Broadcaster {
	return b.Channel(channel.BroadcastChannelRoute(), handler)
}

// GetChannels is every registered channel, by pattern. The map is a copy.
func (b *Broadcaster) GetChannels() map[string]ChannelHandler {
	b.mu.RLock()
	defer b.mu.RUnlock()

	registered := make(map[string]ChannelHandler, len(b.channels))
	for pattern, handler := range b.channels {
		registered[pattern] = handler
	}

	return registered
}

// ResolveAuthenticatedUser is the user payload for the incoming connection, or
// nil when no callback was registered.
//
// See https://pusher.com/docs/channels/library_auth_reference/auth-signatures
// for the document the client expects.
func (b *Broadcaster) ResolveAuthenticatedUser(ctx context.Context, r *http.Request) (any, error) {
	b.mu.RLock()
	callback := b.authenticatedUserCallback
	b.mu.RUnlock()

	if callback == nil {
		return nil, nil
	}

	return callback(ctx, r)
}

// ResolveAuthenticatedUserUsing registers the callback that answers who the
// connection belongs to.
func (b *Broadcaster) ResolveAuthenticatedUserUsing(callback func(ctx context.Context, r *http.Request) (any, error)) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.authenticatedUserCallback = callback
}

// VerifyUserCanAccessChannel walks the registered patterns and lets the first
// one that matches decide.
//
// This is where a channel becomes an authorization decision. The handler is
// wrapped in an auth.Policy and run through auth.Authorize, so the refusal is
// auth.ErrForbidden and the success is an auth.Grant. That is not decoration:
// the Grant carries the tenant every published channel name is built from, and
// nothing in this framework reaches tenant-scoped data without one.
//
// The subject is not an argument. It comes from the context, where the session
// middleware put it, which is what makes it impossible for the request being
// authorized to name the subject it is authorized as.
//
// It returns the raw handler result, not a response. Go has no virtual dispatch
// through an embedded struct, so the driver applies its own
// ValidAuthenticationResponse to what comes back -- see [Broadcaster].
func (b *Broadcaster) VerifyUserCanAccessChannel(ctx context.Context, channel string) (auth.Grant, any, error) {
	subject, ok := b.RetrieveUser(ctx, channel)
	if !ok {
		return auth.Grant{}, nil, fmt.Errorf("%w: nothing on the context says who is asking for %s", auth.ErrForbidden, channel)
	}

	b.mu.RLock()
	order := append([]string(nil), b.order...)
	handlers := b.channels
	patterns := b.patterns
	b.mu.RUnlock()

	for _, pattern := range order {
		if !matches(patterns[pattern], channel) {
			continue
		}

		policy := &channelPolicy{handler: b.NormalizeChannelHandlerToCallable(handlers[pattern])}
		resource := ChannelAuthorization{Name: channel, Parameters: b.ExtractAuthParameters(pattern, channel)}

		g, err := auth.Authorize(ctx, policy, subject, ChannelJoin, resource)
		if errors.Is(err, ErrChannelUndecided) {
			continue
		}
		if err != nil {
			return auth.Grant{}, nil, err
		}

		return g, policy.result, nil
	}

	return auth.Grant{}, nil, fmt.Errorf("%w: no channel is registered for %s", auth.ErrForbidden, channel)
}

// ExtractAuthParameters is the {placeholders} of the pattern, filled in from
// the channel that was asked for.
//
// The values are strings and stay strings: nothing here turns "17" into an
// Order. A channel handler that wants the order loads it through a repository,
// with the Grant it is about to be given.
func (b *Broadcaster) ExtractAuthParameters(pattern, channel string) map[string]string {
	b.mu.RLock()
	compiled, ok := b.patterns[pattern]
	b.mu.RUnlock()

	if !ok {
		compiled = compileChannelPattern(pattern)
	}
	if compiled == nil {
		return map[string]string{}
	}

	match := compiled.FindStringSubmatch(channel)
	if match == nil {
		return map[string]string{}
	}

	parameters := map[string]string{}
	for i, name := range compiled.SubexpNames() {
		// An unnamed group has an empty name, which drops the whole-match group
		// and any group the pattern did not name.
		if name == "" || i >= len(match) {
			continue
		}
		parameters[name] = match[i]
	}

	return parameters
}

// NormalizeChannelHandlerToCallable turns any [ChannelHandler] into the
// function form.
//
// A nil handler becomes one that declines, so a pattern registered with nothing
// behind it does not panic on the subscription that matches it.
func (b *Broadcaster) NormalizeChannelHandlerToCallable(handler ChannelHandler) ChannelHandlerFunc {
	if handler == nil {
		return func(context.Context, auth.Subject, map[string]string) (any, error) { return nil, nil }
	}
	if callable, ok := handler.(ChannelHandlerFunc); ok {
		return callable
	}

	return handler.Join
}

// RetrieveUser is who is asking: auth.SubjectFrom(ctx), the subject the session
// middleware put on the context.
//
// The channel is a parameter so that a driver could answer differently per
// channel; nothing reads it today.
func (b *Broadcaster) RetrieveUser(ctx context.Context, channel string) (auth.Subject, bool) {
	return auth.SubjectFrom(ctx)
}

// FormatChannels is the channels as the strings a driver puts on the wire.
//
// It takes the Grant, so a driver cannot name a channel without a tenant. That
// matters because it is promoted into every driver: a version that dropped the
// tenant would be inherited in silence by the next driver written by copying
// another one, and `d.FormatChannels(channels)` would have compiled.
// [RedisBroadcaster.FormatChannels] shadows this only to put the Redis key
// prefix in front of what it answers -- it does not decide the tenant a second
// time.
func (b *Broadcaster) FormatChannels(g auth.Grant, channels []broadcasting.Channel) ([]string, error) {
	return broadcasting.TenantChannels(g, channels)
}

// ChannelNameMatchesPattern reports whether a channel name matches a registered
// pattern.
//
// It carries no tenant and needs none: both arguments are already normalized
// names, which is what [UsePusherChannelConventions.NormalizeChannelName]
// answers and what the walk in [Broadcaster.VerifyUserCanAccessChannel] matches
// against.
func (b *Broadcaster) ChannelNameMatchesPattern(channel, pattern string) bool {
	b.mu.RLock()
	compiled, ok := b.patterns[pattern]
	b.mu.RUnlock()

	if !ok {
		compiled = compileChannelPattern(pattern)
	}

	return matches(compiled, channel)
}

// channelPolicy is the registered handler seen as an auth.Policy, which is the
// only way a decision is made in this framework.
type channelPolicy struct {
	handler ChannelHandlerFunc
	// result is the handler's result, kept so Auth can hand it to
	// ValidAuthenticationResponse. auth.Policy answers an error and nothing
	// else, which is right -- a policy decides, it does not return data -- so
	// the presence payload is carried out here rather than through the Grant.
	result any
}

// Can is auth.Policy.Can: it asks the channel handler and translates its three
// answers into the one an authorization gives.
func (p *channelPolicy) Can(ctx context.Context, s auth.Subject, a auth.Action, resource ChannelAuthorization) error {
	result, err := p.handler(ctx, s, resource.Parameters)
	if err != nil {
		return err
	}
	if result == nil {
		return ErrChannelUndecided
	}
	if allowed, ok := result.(bool); ok && !allowed {
		return fmt.Errorf("%s is not open to this subject", resource.Name)
	}

	p.result = result

	return nil
}

// channelPlaceholder finds the {placeholders} of a registered pattern.
var channelPlaceholder = regexp.MustCompile(`\{(.*?)\}`)

// groupName is what Go's regexp accepts as a capture group name. A placeholder
// that is not one still matches, it just does not come back as a parameter.
var groupName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// compileChannelPattern turns "orders.{orderId}" into an anchored expression
// with one named capture group per placeholder.
//
// One expression serves both the match and the parameter extraction, because
// two spellings of the same pattern is how a channel matches for the walk and
// then yields no parameters. Everything outside a placeholder is quoted, so a
// channel name is never read as an expression.
func compileChannelPattern(pattern string) *regexp.Regexp {
	var expression strings.Builder
	expression.WriteString("^")

	last := 0
	for _, location := range channelPlaceholder.FindAllStringSubmatchIndex(pattern, -1) {
		expression.WriteString(regexp.QuoteMeta(pattern[last:location[0]]))

		name := pattern[location[2]:location[3]]
		if groupName.MatchString(name) {
			expression.WriteString("(?P<" + name + ">[^.]+)")
		} else {
			expression.WriteString("[^.]+")
		}
		last = location[1]
	}
	expression.WriteString(regexp.QuoteMeta(pattern[last:]))
	expression.WriteString("$")

	compiled, err := regexp.Compile(expression.String())
	if err != nil {
		return nil
	}

	return compiled
}

// matches answers false for a pattern that would not compile, so a malformed
// registration denies rather than matching everything.
func matches(pattern *regexp.Regexp, channel string) bool {
	return pattern != nil && pattern.MatchString(channel)
}
