package hashing

// AuthHasher is a [Hasher] behind the three methods hesape/auth declares as
// auth.Hasher, so that a real hasher can be wired into a guard, a user provider
// and a password broker.
//
// The two Check methods are not the same method. auth.Hasher declares
//
//	Check(value, hashedValue string) bool
//
// and every hasher in this package declares
//
//	Check(value, hashedValue string, options ...Options) (bool, error)
//
// so no hasher here satisfies auth.Hasher on its own. [AbstractHasher] does
// declare the narrow signature, but a method on the outer type shadows the
// promoted one, so embedding does not bridge it either.
//
// Neither side widens. auth.Hasher is the narrow contract the consuming package
// declares: a guard checks a password, a user provider rehashes one, and
// neither has a per-call cost factor to pass. Widening it would make the root of
// auth name [Options] and [Params], which means importing this package -- and
// the root of auth imports nothing outside the standard library on purpose,
// because every package that scopes itself by tenant imports it to read
// auth.Tenant off a Grant. The wide signature stays too, because the per-call
// options are real: Rounds, Memory, Time and Threads are what a test suite
// hashes at cost 4 with.
//
// So this is an adapter and not a second hasher: it hashes nothing of its own
// and holds no parameters of its own. It forwards to the hasher it was given,
// which hashes with the parameters it was constructed with.
type AuthHasher struct {
	// hasher is the hasher every call is forwarded to.
	hasher Hasher
}

// ForAuth returns h behind the interface hesape/auth consumes.
//
//	provider := users.NewEloquentUserProvider(
//		hashing.ForAuth(hashing.NewBcryptHasher()), model, newQuery, "acme")
//
// A nil hasher is bcrypt on its own defaults, which is the same reading
// [NewHashManager] gives a nil [Config]. It is not a hasher of this adapter's
// own: it is one of the three this package already builds.
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

// Make hashes value, and is the Make of auth.Hasher.
//
// No options are passed, and there is nowhere to pass any from: the narrow
// contract has no argument for them. That is the right shape for the callers it
// has -- a per-call cost factor at a sign-in is a caller weakening the hash of
// the password it is about to store -- and the parameters that do apply are the
// ones the hasher was constructed with.
func (a *AuthHasher) Make(value string) (string, error) {
	return a.hasher.Make(value)
}

// Check reports whether this password matches this hash, and is the Check of
// auth.Hasher.
//
// An error from the underlying hasher is false. There are two of them and
// neither is a right password: [ErrWrongAlgorithm], when the hasher was built
// with Verify and the stored hash was written by another algorithm, and an
// unreadable hash, which is a corrupt password column.
//
// The sign-in is refused in both cases because there is nowhere to put the
// error: auth.UserProvider.ValidateCredentials answers bool, and so does this
// contract. Refusing is the only reading that is safe in the direction it
// fails -- a column nobody can verify must not authenticate anybody.
func (a *AuthHasher) Check(value, hashedValue string) bool {
	ok, err := a.hasher.Check(value, hashedValue)
	return err == nil && ok
}

// NeedsRehash reports that the hash was written with parameters other than the
// ones in force, so the next sign-in that proves the plain password should
// rewrite it. It is the NeedsRehash of auth.Hasher.
func (a *AuthHasher) NeedsRehash(hashedValue string) bool {
	return a.hasher.NeedsRehash(hashedValue)
}
