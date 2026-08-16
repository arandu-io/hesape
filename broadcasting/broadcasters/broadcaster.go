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
// is a Grant that passes Check in one package and fails it in the other
// (RULE 9).
const ChannelJoin = broadcasting.ChannelJoin

// ErrChannelUndecided is what a channel handler returns to say the pattern it
// was registered under does not apply after all, so the search should carry on
// to the next one.
//
// It is the PHP's null result. Broadcaster::verifyUserCanAccessChannel treats
// three answers differently: false denies immediately, a truthy value allows,
// and null keeps looking. A Go handler returning (nil, nil) means the same
// thing, and this error is what that turns into once auth.Authorize has been
// asked -- a Policy that returns nil has allowed, so "keep looking" has to be
// an error to travel back out.
var ErrChannelUndecided = errors.New("broadcasting: the channel handler did not decide")

// ChannelAuthorization is the resource a channel Policy decides about.
//
// It has no counterpart in Illuminate: the PHP spreads the pattern's parameters
// across the callback's arguments, which needs reflection over the callback's
// signature. Go has no such thing, so the parameters arrive as a map and the
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

// ChannelHandler is what Broadcaster::channel registers: the thing that decides
// whether a subject may listen on a channel.
//
// The PHP accepts a callable or the name of a class with a join() method, and
// normalizeChannelHandlerToCallable turns the second into the first. This is
// that class, and [ChannelHandlerFunc] is that callable.
type ChannelHandler interface {
	// Join is the class-based channel's join($user, ...$parameters).
	//
	// A nil error allows. A non-nil error denies, and is the PHP's `return
	// false`. A nil result with a nil error is the PHP's `return null`: this
	// pattern declines to decide and the next one is tried -- see
	// [ErrChannelUndecided].
	//
	// The value returned on success is the PHP's $result, which
	// ValidAuthenticationResponse turns into the presence channel's user_info.
	// `true` is the ordinary answer for a private channel.
	Join(ctx context.Context, s auth.Subject, parameters map[string]string) (any, error)
}

// ChannelHandlerFunc is a function that is a [ChannelHandler]: the callable
// form Broadcaster::channel accepts.
type ChannelHandlerFunc func(ctx context.Context, s auth.Subject, parameters map[string]string) (any, error)

// Join calls f, so a plain function satisfies [ChannelHandler].
func (f ChannelHandlerFunc) Join(ctx context.Context, s auth.Subject, parameters map[string]string) (any, error) {
	return f(ctx, s, parameters)
}

// Broadcaster is the abstract Illuminate\Broadcasting\Broadcasters\Broadcaster:
// the channel registry and the authorization walk every driver shares.
//
// The PHP is an abstract class the drivers extend; here it is a struct they
// embed. The one thing embedding cannot do is call back into the child -- Go
// has no virtual dispatch -- so verifyUserCanAccessChannel does not call the
// driver's validAuthenticationResponse. [Broadcaster.VerifyUserCanAccessChannel]
// answers the raw handler result instead, and each driver's Auth passes it
// through its own ValidAuthenticationResponse, which is the same two steps in
// the same order.
//
// A Broadcaster is safe for concurrent use: channels are registered at start-up
// and read on every subscription.
type Broadcaster struct {
	mu sync.RWMutex
	// authenticatedUserCallback is the protected $authenticatedUserCallback.
	authenticatedUserCallback func(ctx context.Context, r *http.Request) (any, error)
	// channels is the protected $channels.
	channels map[string]ChannelHandler
	// patterns caches the compiled form of each registered pattern.
	patterns map[string]*regexp.Regexp
	// order is the registration order. PHP arrays keep it and Go maps do not,
	// and it decides which of two overlapping patterns answers first.
	order []string
}

// Channel is Broadcaster::channel: register a channel authenticator.
//
// The PHP's third argument, $options, has no twin. The only key Illuminate
// reads out of it is `guards`, which picks between several authentication
// guards; there is one subject here and it comes from the context
// (auth.SubjectFrom), so there is nothing to pick between. That takes
// retrieveChannelOptions with it.
//
// Registering the same pattern twice replaces the handler, as assigning to the
// same array key does.
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

// ChannelFor is Broadcaster::channel given a HasBroadcastChannel, which the PHP
// resolves with broadcastChannelRoute() -- the pattern, not the instance's own
// name.
func (b *Broadcaster) ChannelFor(channel broadcasting.HasBroadcastChannel, handler ChannelHandler) *Broadcaster {
	return b.Channel(channel.BroadcastChannelRoute(), handler)
}

// GetChannels is Broadcaster::getChannels: every registered channel, by
// pattern.
func (b *Broadcaster) GetChannels() map[string]ChannelHandler {
	b.mu.RLock()
	defer b.mu.RUnlock()

	registered := make(map[string]ChannelHandler, len(b.channels))
	for pattern, handler := range b.channels {
		registered[pattern] = handler
	}

	return registered
}

// ResolveAuthenticatedUser is Broadcaster::resolveAuthenticatedUser: the user
// payload for the incoming connection, or nil when no callback was registered.
//
// See https://pusher.com/docs/channels/library_auth_reference/auth-signatures,
// which is the link the PHP carries.
func (b *Broadcaster) ResolveAuthenticatedUser(ctx context.Context, r *http.Request) (any, error) {
	b.mu.RLock()
	callback := b.authenticatedUserCallback
	b.mu.RUnlock()

	if callback == nil {
		return nil, nil
	}

	return callback(ctx, r)
}

// ResolveAuthenticatedUserUsing is Broadcaster::resolveAuthenticatedUserUsing:
// register the callback that answers who the connection belongs to.
func (b *Broadcaster) ResolveAuthenticatedUserUsing(callback func(ctx context.Context, r *http.Request) (any, error)) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.authenticatedUserCallback = callback
}

// VerifyUserCanAccessChannel is Broadcaster::verifyUserCanAccessChannel: walk
// the registered patterns and let the first one that matches decide.
//
// This is where a channel becomes an authorization decision. The PHP calls the
// registered callback and reads its return value; here the callback is wrapped
// in an auth.Policy and run through auth.Authorize, so the refusal is
// auth.ErrForbidden and the success is an auth.Grant. That is not decoration:
// the Grant carries the tenant that every published channel name is built from
// (RULE 14), and nothing in this framework reaches tenant-scoped data without
// one.
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

// ExtractAuthParameters is Broadcaster::extractAuthParameters: the
// {placeholders} of the pattern, filled in from the channel that was asked for.
//
// Five of the PHP's helpers end here, and every one of them is reflection or
// the container -- reason (1) and reason (2) of ADR 0056. extractParameters and
// extractParametersFromClass read the callback's signature to know what to pass
// and in which order; Go cannot inspect a func's parameter names, and does not
// need to, because the parameters arrive as a map. resolveBinding,
// resolveExplicitBindingIfPossible, resolveImplicitBindingIfPossible and
// isImplicitlyBindable turn "17" into an Order by asking the container for a
// binding registrar and instantiating the class named in the signature; route
// model binding is the router's here, and a channel handler that wants the
// order loads it through a repository with the Grant it is about to be given.
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
		// The PHP rejects numeric keys, which is how it drops the whole-match
		// group and any group the pattern did not name. An unnamed group has an
		// empty name here, and that is the same rejection.
		if name == "" || i >= len(match) {
			continue
		}
		parameters[name] = match[i]
	}

	return parameters
}

// NormalizeChannelHandlerToCallable is
// Broadcaster::normalizeChannelHandlerToCallable.
//
// The PHP wraps a class name in a closure that resolves the class out of the
// container and calls join() on it. There is no container (ADR 0001) and no
// class names to resolve, so what is left is the conversion itself: any
// [ChannelHandler] becomes the function form. A nil handler becomes one that
// declines, so a pattern registered with nothing behind it does not panic on
// the subscription that matches it.
func (b *Broadcaster) NormalizeChannelHandlerToCallable(handler ChannelHandler) ChannelHandlerFunc {
	if handler == nil {
		return func(context.Context, auth.Subject, map[string]string) (any, error) { return nil, nil }
	}
	if callable, ok := handler.(ChannelHandlerFunc); ok {
		return callable
	}

	return handler.Join
}

// RetrieveUser is Broadcaster::retrieveUser: who is asking.
//
// The PHP reads $request->user(), optionally through one of the guards named in
// the channel's options. Here it is auth.SubjectFrom(ctx) -- the subject the
// session middleware put on the context -- and there are no guards to choose
// between, which is why the options argument is gone from Channel.
//
// The channel is kept as a parameter because the PHP has it and because a
// future reason to answer differently per channel would want it; nothing reads
// it today.
func (b *Broadcaster) RetrieveUser(ctx context.Context, channel string) (auth.Subject, bool) {
	return auth.SubjectFrom(ctx)
}

// FormatChannels is Broadcaster::formatChannels: the channels as the strings a
// driver puts on the wire.
//
// What was wrong: this method used to take `channels []broadcasting.Channel`
// and answer `channel.String()` for each one, dropping the tenant. It had no
// caller and no test, and Go has no virtual dispatch -- it was promoted into
// LogBroadcaster and NullBroadcaster, neither of which shadows it, and only
// RedisBroadcaster shadowed it with the version that takes a Grant. There was
// no call proving it wrong, and that was the danger: the next driver written by
// copying one of the other two would have inherited a tenant-less name
// formatter in silence, and `d.FormatChannels(channels)` would have compiled.
//
// It now takes the Grant, so it cannot. Every driver inherits the tenant, and
// [RedisBroadcaster.FormatChannels] shadows this only to put the Redis key
// prefix in front of what this answers -- it does not decide the tenant a
// second time (RULE 9).
func (b *Broadcaster) FormatChannels(g auth.Grant, channels []broadcasting.Channel) ([]string, error) {
	return broadcasting.TenantChannels(g, channels)
}

// ChannelNameMatchesPattern is Broadcaster::channelNameMatchesPattern.
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
	// result is the handler's $result, kept so Auth can hand it to
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

// channelPlaceholder is the PHP's /\{(.*?)\}/.
var channelPlaceholder = regexp.MustCompile(`\{(.*?)\}`)

// groupName is what Go's regexp accepts as a capture group name. A placeholder
// that is not one still matches, it just does not come back as a parameter.
var groupName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// compileChannelPattern turns "orders.{orderId}" into the anchored expression
// both channelNameMatchesPattern and extractChannelKeys build in the PHP.
//
// The PHP builds two: channelNameMatchesPattern escapes the dots and anchors
// both ends, extractChannelKeys escapes nothing and anchors only the start. One
// expression is built here and used for both, because two spellings of the same
// pattern is how a channel matches for the walk and then yields no parameters
// (RULE 9). Everything outside a placeholder is quoted, so a channel name is
// never read as an expression.
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
