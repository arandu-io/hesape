// Package access mirrors Illuminate\Auth\Access.
//
// The files it answers to, in the clone at
// laravel_illuminate/auth/Access:
//
//	AuthorizationException.php
//	Gate.php
//	HandlesAuthorization.php
//	Response.php
//
// It is the Laravel vocabulary for authorization: a [Gate] holding abilities and
// policies, a [Response] carrying the sentence behind an answer, and an
// [AuthorizationError] for the failure. Define an ability, then ask:
//
//	gate := access.NewGate()
//	gate.Define("posts.update", func(ctx context.Context, user auth.Subject, arguments ...any) any {
//		post, ok := arguments[0].(Post)
//		return ok && post.AuthorID == user.ID
//	})
//
//	if gate.Allows(ctx, subject, "posts.update", post) {
//		// draw the button
//	}
//
// # The Gate is not a second way to authorize
//
// A framework with an auth.Grant and a Gate has two authorization stories, and
// the weaker one wins whenever somebody is in a hurry (RULE 9). So there is one:
// the Gate does not decide, it delegates. [Gate.Authorize] wraps the ability as
// an auth.Policy and hands it to auth.Authorize, which is the only function that
// builds a Grant:
//
//	type abilityPolicy struct {
//		fn func(ctx context.Context, s auth.Subject, args []any) error
//	}
//
//	func (p abilityPolicy) Can(ctx context.Context, s auth.Subject, a auth.Action, args []any) error {
//		return p.fn(ctx, s, args)
//	}
//
//	func (g *Gate) Authorize(ctx context.Context, s auth.Subject, ability string, arguments ...any) (auth.Grant, error) {
//		// ...
//		return auth.Authorize[[]any](ctx, abilityPolicy{fn: check}, s, auth.Action(ability), arguments)
//	}
//
// What that buys is the whole thesis of the framework. auth.Grant has only
// unexported fields, every repository signature demands one, and this package
// has no way to forge one either -- so an ability defined here reaches the
// database through the same door a hand-written policy does, and the anonymous
// subject auth.Authorize refuses is refused here too, before the ability is ever
// consulted. Not the developer guaranteeing the architecture: the compiler.
//
// The Gate's own answers -- [Gate.Allows], [Gate.Check], [Gate.Any] -- issue
// nothing, and that is the point of them: they are for the view, which needs to
// know whether to draw a button and has no use for a Grant. A handler that acts
// on one still has to call [Gate.Authorize] to reach a repository.
//
// # What is not ported, and why
//
// Gate::setContainer sets the container the Gate resolves policies and the event
// dispatcher out of. Reason 2 -- a method that exists only to serve the
// container, which ADR 0001 rejected.
//
// Gate::resolvePolicy is `$this->container->make($class)`, one line whose whole
// content is the container. Reason 2. [Gate.Policy] takes the policy itself.
//
// Gate::guessPolicyNamesUsing replaces the convention that turns App\Models\Post
// into App\Policies\PostPolicy, and Gate::guessPolicyName is the convention
// itself. Both resolve a class by building its name as a string. Reasons 1 and 2:
// Go has no class lookup by name and no container to build one from. Register the
// policy with [Gate.Policy].
//
// Gate::getPolicyFromAttribute reads a #[UsePolicy] attribute off the model
// class. Reason 1 -- Go has no attributes. Same replacement.
//
// The 'Class@method' and [Class, method] forms of Gate::define, and the
// $stringCallbacks they are remembered in, name a class to resolve by string.
// Reasons 1 and 2. [Gate.Resource] covers what they were for, taking the policy
// as a value.
//
// Gate::canBeCalledWithUser, methodAllowsGuests, callbackAllowsGuests and
// parameterAllowsGuests reflect over a callback's first parameter to decide
// whether a guest may reach it: in PHP an unauthenticated user is null, and only
// a callback whose parameter is nullable is called with one. Reason 1. Nothing
// here is called with a null subject -- auth.Guest is a subject like any other,
// and an ability tells it apart with user.IsGuest, so the callback decides
// instead of its signature. A policy that says nothing about guests denies them.
//
// Gate::dispatchGateEvaluatedEvent fires GateEvaluated through the dispatcher it
// resolves out of the container. Reason 2 for the resolution, and only for it:
// the event is written, in auth/access/events, and [Gate.Observe] is where the
// destination arrives -- as an argument, since there is no container to resolve
// one from. A Gate given none does no work.
//
// HandlesAuthorization's allow() and deny() are protected trait methods. An
// unexported method promoted from an embedded struct is not callable by the
// package that embeds it, so they are [Allow] and [Deny], the package functions.
// The two public ones are on [HandlesAuthorization].
//
// Illuminate\Contracts\Auth\Access\Gate, the interface, is not declared: Go
// satisfies an interface structurally, which is ADR 0045.
//
// Everything in Response.php and AuthorizationException.php is here.
package access
