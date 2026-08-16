// Package access is the ability-and-policy vocabulary for authorization: a
// [Gate] holding abilities and policies, a [Response] carrying the sentence
// behind an answer, and an [AuthorizationError] for the failure.
//
// Define an ability, then ask:
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
// A framework with an auth.Grant and a Gate would have two authorization
// stories, and the weaker one wins whenever somebody is in a hurry. So there is
// one: the Gate does not decide, it delegates. [Gate.Authorize] wraps the
// ability as an auth.Policy and hands it to auth.Authorize, which is the only
// function that builds a Grant:
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
// # A guest is a subject
//
// Nothing here is ever called with an absent user. auth.Guest is a subject like
// any other, and an ability tells it apart by asking user.IsGuest, so the
// callback decides rather than its signature. An ability that says nothing
// about guests denies them.
//
// # Allow and Deny are functions
//
// An unexported method promoted from an embedded struct is not callable by the
// package that embeds it, so the two shorthands that build a [Response] are the
// package functions [Allow] and [Deny] rather than methods. The two that are
// exported are on [HandlesAuthorization].
package access
