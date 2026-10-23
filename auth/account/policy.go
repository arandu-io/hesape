package account

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/auth"
)

// PolicyFunc is a user policy over Account, one of the shapes Config.Policy
// takes.
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
// When policy implements auth.Writes, so does what PolicyOf returns, with the
// same answers: a Grant the account rules are issued for an action the policy
// says does not write only reads, as it would from the policy itself. A policy
// that does not implement it is returned as a PolicyFunc, and its Grants write.
//
// It is the one generic declaration here, and it is a function, not a type:
// its body is the conversion and nothing else, so what a consuming package
// compiles for it is that conversion and nothing of the account rules, which
// are compiled once.
func PolicyOf[U Account](policy auth.Policy[U]) auth.Policy[Account] {
	can := PolicyFunc(func(ctx context.Context, s auth.Subject, a auth.Action, user Account) error {
		typed, ok := user.(U)
		if !ok {
			return fmt.Errorf("account: the policy decides about %T, and was asked about %T", typed, user)
		}
		return policy.Can(ctx, s, a, typed)
	})
	if writes, ok := policy.(auth.Writes); ok {
		return writingPolicy{PolicyFunc: can, writes: writes}
	}
	return can
}

// writingPolicy is an adapted user policy that declares which actions write.
// It is a struct and not a generic type, so it is compiled here once.
type writingPolicy struct {
	PolicyFunc
	writes auth.Writes
}

// Writes answers what the adapted policy answers.
func (p writingPolicy) Writes(a auth.Action) bool { return p.writes.Writes(a) }

var (
	_ auth.Policy[Account] = PolicyFunc(nil)
	_ auth.Policy[Account] = writingPolicy{}
	_ auth.Writes          = writingPolicy{}
)
