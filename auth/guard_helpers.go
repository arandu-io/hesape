package auth

// GuardHelpers is Illuminate\Auth\GuardHelpers.
//
// The PHP is a trait, and every guard in this package writes `use
// GuardHelpers`. Go has no traits, so it is a struct the guards embed, and
// embedding promotes these methods onto SessionGuard, TokenGuard and
// RequestGuard exactly as the trait mixed them in. The names and the bodies are
// the PHP's; only the language feature carrying them changed, which is why the
// trait is transposed here rather than listed in doc.go as skipped.
//
// One thing embedding cannot do that a trait can: the trait's methods call
// $this->user(), and that is the *guard's* user(), not the trait's. A promoted
// method in Go sees the struct it was declared on, never the struct it was
// embedded in. So each guard hands its own User over at construction, in
// resolveUser -- the house pattern for a trait's abstract method.
//
// Its two fields are the trait's two properties, and they are unexported
// because the PHP's are protected and every guard that reads them is in this
// package.
type GuardHelpers struct {
	// user is the trait's $user: the currently authenticated user, already
	// resolved. It is nil until something resolves one.
	user Authenticatable

	// provider is the trait's $provider.
	provider UserProvider

	// resolveUser is the guard's own user(), which the trait's methods call
	// through $this. A guard sets it in its constructor. When it is nil -- a
	// GuardHelpers used on its own, which no guard does -- the helpers fall
	// back to the resolved user, so nothing here panics on a zero value.
	resolveUser func() Authenticatable
}

// Authenticate is GuardHelpers::authenticate: the user, or the failure.
//
// The PHP throws AuthenticationException. This returns it, which is ADR 0044's
// second mechanical change, and the error is *AuthenticationError so a caller
// can read the guards off it with errors.As.
func (g *GuardHelpers) Authenticate() (Authenticatable, error) {
	if user := g.currentUser(); user != nil {
		return user, nil
	}
	return nil, NewAuthenticationError("", nil, "")
}

// HasUser is GuardHelpers::hasUser: a user is already resolved.
//
// It asks about the field, not about the guard's user(), so it never goes to
// the session or the database to answer.
func (g *GuardHelpers) HasUser() bool {
	return g.user != nil
}

// Check is GuardHelpers::check: somebody is signed in.
func (g *GuardHelpers) Check() bool {
	return g.currentUser() != nil
}

// Guest is GuardHelpers::guest: nobody is.
func (g *GuardHelpers) Guest() bool {
	return !g.Check()
}

// ID is GuardHelpers::id: the authenticated user's identifier, or nil.
func (g *GuardHelpers) ID() any {
	if user := g.currentUser(); user != nil {
		return user.GetAuthIdentifier()
	}
	return nil
}

// SetUser is GuardHelpers::setUser.
//
// The PHP returns $this to be chained. This returns nothing, because the Guard
// contract's SetUser returns nothing and because a promoted method that
// returned something would hand back the trait, not the guard that embedded it.
func (g *GuardHelpers) SetUser(user Authenticatable) {
	g.user = user
}

// ForgetUser is GuardHelpers::forgetUser: drop the resolved user without
// touching the session or the cookie.
//
// It returns nothing, for the reason [GuardHelpers.SetUser] gives.
func (g *GuardHelpers) ForgetUser() {
	g.user = nil
}

// GetProvider is GuardHelpers::getProvider.
func (g *GuardHelpers) GetProvider() UserProvider {
	return g.provider
}

// SetProvider is GuardHelpers::setProvider.
func (g *GuardHelpers) SetProvider(provider UserProvider) {
	g.provider = provider
}

// currentUser is the $this->user() the trait's methods call: the guard's own
// resolution when there is one, the resolved user otherwise.
func (g *GuardHelpers) currentUser() Authenticatable {
	if g.resolveUser != nil {
		return g.resolveUser()
	}
	return g.user
}
