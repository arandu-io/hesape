package account

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/auth"
)

// PolicyFunc is a user policy over Account, the shape Config.Policy takes.
//
// It is a function type rather than an interface the application implements,
// so the application's own policy -- written over its own *User, the way every
// policy is -- reaches this package through PolicyOf without changing.
type PolicyFunc func(ctx context.Context, s auth.Subject, a auth.Action, user Account) error

// Can implements auth.Policy[Account].
func (f PolicyFunc) Can(ctx context.Context, s auth.Subject, a auth.Action, user Account) error {
	return f(ctx, s, a, user)
}

// PolicyOf adapts the application's user policy to Config.Policy:
//
//	Policy: account.PolicyOf(policies.UserPolicy{}),
//
// Every account this package hands a policy is built by the application's
// users query, so it is a U; anything else is refused rather than decided
// about.
//
// It is the one generic declaration here, and it is a function, not a type:
// its body is the conversion and nothing else, so what a consuming package
// compiles for it is that conversion and nothing of the account rules, which
// are compiled once.
func PolicyOf[U Account](policy auth.Policy[U]) PolicyFunc {
	return func(ctx context.Context, s auth.Subject, a auth.Action, user Account) error {
		typed, ok := user.(U)
		if !ok {
			return fmt.Errorf("account: the policy decides about %T, and was asked about %T", typed, user)
		}
		return policy.Can(ctx, s, a, typed)
	}
}

var _ auth.Policy[Account] = PolicyFunc(nil)
