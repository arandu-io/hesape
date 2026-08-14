package hashing

// AuthHasher is a [Hasher] behind the three methods hesape/auth declares as
// auth.Hasher, so that a real hasher can be wired into a guard, a user provider
// and a password broker.
//
// # What was wrong
//
// auth.Hasher declares
//
//	Check(value, hashedValue string) bool
//
// and every hasher in this package declares
//
//	Check(value, hashedValue string, options ...Options) (bool, error)
//
// Those are two different methods, so no hasher in this package has ever
// satisfied auth.Hasher. BcryptHasher embeds AbstractHasher, whose Check does
// have the narrow signature, but a method declared on the outer type shadows the
// promoted one -- so the embedding hid the mismatch instead of resolving it.
//
// The input that proved it is a line of wiring that did not compile:
//
//	users.NewEloquentUserProvider(hashing.NewBcryptHasher(), model, newQuery, "acme")
//	// cannot use hashing.NewBcryptHasher() ... as auth.Hasher value:
//	// wrong type for method Check
//
// and the only type in the repository that did satisfy auth.Hasher was the
// fakeHasher of auth/guards_test.go, which hashes by prefixing the word
// "hashed:". Authentication had a test double and no production implementation.
//
// # Which side ceded, and why
//
// auth.Hasher stays as it is. It is the narrow contract the consuming package
// declares (see auth/contracts.go): a guard checks a password, a user provider
// rehashes one, and neither has a per-call cost factor to pass. Widening it to
// the four-method shape would make the root of auth name Options and Params,
// which means importing this package -- and the root of auth imports nothing
// outside the standard library on purpose, because every package that scopes
// itself by tenant imports it to read auth.Tenant off a Grant.
//
// Illuminate\Hashing's signature stays too. The $options array is real:
// 'rounds', 'memory', 'time' and 'threads' are what a test suite hashes at cost
// 4 with, and Hasher is the mirror of Illuminate\Contracts\Hashing\Hasher.
//
// So the adapter is the third thing, and it is an adapter rather than a second
// hasher (RULE 9): it hashes nothing of its own and holds no parameters of its
// own. It forwards to the hasher it was given, which hashes with the parameters
// it was constructed with.
type AuthHasher struct {
	// hasher is the hasher every call is forwarded to.
	hasher Hasher
}

// ForAuth returns h behind the interface hesape/auth consumes.
//
//	provider := users.NewEloquentUserProvider(
//		hashing.ForAuth(hashing.NewBcryptHasher()), model, newQuery, "acme")
//
// A nil hasher is bcrypt on its class defaults, which is what
// HashManager::getDefaultDriver falls back to -- the same reading
// [NewHashManager] gives a nil Config. It is not a hasher of this adapter's own:
// it is one of the three this package already builds.
//
// A *HashManager satisfies [Hasher], so an application that configures hashing
// through hashing.driver passes the manager here and the guard hashes with
// whatever that key names.
func ForAuth(h Hasher) *AuthHasher {
	if h == nil {
		h = NewBcryptHasher()
	}
	return &AuthHasher{hasher: h}
}

// Hasher returns the hasher this adapter forwards to, so that a caller who needs
// the options argument -- a rehash at a named cost, an Info read -- can reach it
// without keeping a second reference next to the adapter.
func (a *AuthHasher) Hasher() Hasher { return a.hasher }

// Make answers auth.Hasher's Make, which is Hash::make($value).
//
// No options are passed, and there is nowhere to pass any from: the narrow
// contract has no argument for them. That is the right shape for the callers it
// has -- a per-call cost factor at a sign-in is a caller weakening the hash of
// the password it is about to store -- and the parameters that do apply are the
// ones the hasher was constructed with.
func (a *AuthHasher) Make(value string) (string, error) {
	return a.hasher.Make(value)
}

// Check answers auth.Hasher's Check: does this password match this hash.
//
// An error from the underlying hasher is false. There are two of them and
// neither is a right password: [ErrWrongAlgorithm], when the hasher was built
// with 'verify' and the stored hash was written by another algorithm, and an
// unreadable hash, which is a corrupt password column.
//
// The PHP throws a RuntimeException for the first and the request ends in a 500.
// Here the sign-in is refused instead, because there is nowhere to put the
// error: auth.UserProvider.ValidateCredentials answers bool, as
// Illuminate's does, and so does this contract. Refusing is the only reading
// that is safe in the direction it fails -- a column nobody can verify must not
// authenticate anybody.
func (a *AuthHasher) Check(value, hashedValue string) bool {
	ok, err := a.hasher.Check(value, hashedValue)
	return err == nil && ok
}

// NeedsRehash answers auth.Hasher's NeedsRehash: the hash was written with
// parameters other than the ones in force, so the next sign-in that proves the
// plain password should rewrite it.
func (a *AuthHasher) NeedsRehash(hashedValue string) bool {
	return a.hasher.NeedsRehash(hashedValue)
}
