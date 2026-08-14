package users_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/users"
	"github.com/arandu-io/hesape/database/query"
)

// The tenant every provider in this file is configured with. It comes from here
// -- the wiring -- and from nowhere else, which is the whole of RULE 14 as far
// as a user provider is concerned.
const tenant = "acme"

// newEloquentProvider builds the provider under test over the fake connection.
func newEloquentProvider(connection *fakeConnection, hasher auth.Hasher) *users.EloquentUserProvider {
	return users.NewEloquentUserProvider(hasher, newTestUser, connection.users, tenant)
}

// ---------------------------------------------------------------------------
// validateCredentials
// ---------------------------------------------------------------------------

// TestValidateCredentialsAcceptsAPasswordThatIsNotAString is the regression for
// the strict type assertion in passwordOf.
//
// The input is a login body with the password written without quotes. It is not
// hypothetical: any client that builds the JSON from a numeric field sends it,
// and encoding/json decodes it into a float64. The assertion
// credentials["password"].(string) failed, validateCredentials answered false,
// and the account could never be signed in to -- with the guard reporting the
// same refusal a wrong password gets.
//
// The PHP coerces: password_verify is declared string and Illuminate does not
// declare strict_types, so 12345 arrives as "12345" and the sign-in works.
func TestValidateCredentialsAcceptsAPasswordThatIsNotAString(t *testing.T) {
	hasher := cheapHasher()
	user := &testUser{attributes: map[string]any{
		"id":       int64(7),
		"password": mustHash(t, hasher, "12345"),
	}}

	var credentials map[string]any
	if err := json.Unmarshal([]byte(`{"email":"ana@example.com","password":12345}`), &credentials); err != nil {
		t.Fatalf("decoding the login body: %v", err)
	}
	// The shape the decoder actually produced, so the test says what it is
	// about rather than assuming it.
	if _, ok := credentials["password"].(float64); !ok {
		t.Fatalf("the login body decoded the password as %T, want float64", credentials["password"])
	}

	provider := newEloquentProvider(&fakeConnection{}, hasher)

	if !provider.ValidateCredentials(context.Background(), user, credentials) {
		t.Fatal("the password 12345, sent unquoted, did not authenticate the account it hashed")
	}
}

// The other shapes PHP renders on the way into password_verify. A number is the
// one that happens; the rest are here so that the coercion is a rule and not a
// patch for float64.
func TestValidateCredentialsCoercesEveryShapePHPWouldHaveCoerced(t *testing.T) {
	hasher := cheapHasher()

	for name, credential := range map[string]struct {
		plain string
		value any
	}{
		"string":  {"correct horse", "correct horse"},
		"int":     {"12345", 12345},
		"int64":   {"12345", int64(12345)},
		"uint":    {"12345", uint(12345)},
		"float64": {"12345", float64(12345)},
		"decimal": {"1.5", 1.5},
		"bytes":   {"correct horse", []byte("correct horse")},
		"true":    {"1", true},
	} {
		t.Run(name, func(t *testing.T) {
			user := &testUser{attributes: map[string]any{
				"password": mustHash(t, hasher, credential.plain),
			}}
			provider := newEloquentProvider(&fakeConnection{}, hasher)

			if !provider.ValidateCredentials(context.Background(), user, map[string]any{"password": credential.value}) {
				t.Fatalf("%#v did not authenticate against the hash of %q", credential.value, credential.plain)
			}
		})
	}
}

// A value PHP cannot render is a TypeError there and a refusal here. It must
// never authenticate, and it must never panic.
func TestValidateCredentialsRefusesAPasswordThatIsNotAValue(t *testing.T) {
	hasher := cheapHasher()
	user := &testUser{attributes: map[string]any{"password": mustHash(t, hasher, "correct horse")}}
	provider := newEloquentProvider(&fakeConnection{}, hasher)

	for name, value := range map[string]any{
		"missing": nil,
		"list":    []string{"correct horse"},
		"map":     map[string]any{"password": "correct horse"},
	} {
		t.Run(name, func(t *testing.T) {
			if provider.ValidateCredentials(context.Background(), user, map[string]any{"password": value}) {
				t.Fatalf("%#v authenticated", value)
			}
		})
	}

	if provider.ValidateCredentials(context.Background(), user, map[string]any{"email": "ana@example.com"}) {
		t.Fatal("credentials with no password key at all authenticated")
	}
}

// TestValidateCredentialsHandsAnEmptyPasswordToTheHasher is the regression for
// the early return on plain == "".
//
// The PHP has no such return: "" goes into password_verify like any other value
// and is refused there, in the time a bcrypt comparison takes. Returning early
// made the empty password the one refusal that costs nothing, which is a
// difference that can be timed. The claim is not "false" -- it was false before
// too -- it is that the hasher was reached.
func TestValidateCredentialsHandsAnEmptyPasswordToTheHasher(t *testing.T) {
	hasher := newCountingHasher()
	user := &testUser{attributes: map[string]any{"password": mustHash(t, hasher, "correct horse")}}

	provider := newEloquentProvider(&fakeConnection{}, hasher)

	before := hasher.checks
	if provider.ValidateCredentials(context.Background(), user, map[string]any{"password": ""}) {
		t.Fatal("an empty password authenticated")
	}
	if hasher.checks != before+1 {
		t.Fatalf("the hasher was called %d times for an empty password, want 1: it was refused before the comparison", hasher.checks-before)
	}
}

// The one guard the PHP really has on the stored side: an account whose password
// column is empty -- an invite that was never completed -- cannot be signed in
// to by offering an empty password.
func TestValidateCredentialsRefusesAnAccountWithNoPassword(t *testing.T) {
	hasher := newCountingHasher()
	provider := newEloquentProvider(&fakeConnection{}, hasher)
	user := &testUser{attributes: map[string]any{"id": int64(7)}}

	for name, offered := range map[string]any{"empty": "", "something": "correct horse"} {
		t.Run(name, func(t *testing.T) {
			if provider.ValidateCredentials(context.Background(), user, map[string]any{"password": offered}) {
				t.Fatal("an account with an empty password column authenticated")
			}
		})
	}
	if hasher.checks != 0 {
		t.Fatalf("the hasher was called %d times for an account with no password", hasher.checks)
	}
}

// ---------------------------------------------------------------------------
// The whole retrieve-and-validate path, on a real hasher
// ---------------------------------------------------------------------------

// TestEloquentUserProviderAuthenticatesWithARealBcryptHasher is the proof asked
// for by the first defect: a hashing.BcryptHasher -- not a double -- reaches
// auth.UserProvider.ValidateCredentials and authenticates through it.
//
// Before hashing.AuthHasher existed this test did not compile: *BcryptHasher was
// not an auth.Hasher, so there was nothing to pass to the constructor.
func TestEloquentUserProviderAuthenticatesWithARealBcryptHasher(t *testing.T) {
	hasher := cheapHasher()
	hashed := mustHash(t, hasher, "correct horse battery staple")

	connection := (&fakeConnection{}).queue(query.Record{
		"id":       int64(7),
		"email":    "ana@example.com",
		"password": hashed,
	})
	provider := newEloquentProvider(connection, hasher)

	credentials := map[string]any{"email": "ana@example.com", "password": "correct horse battery staple"}

	user, err := provider.RetrieveByCredentials(context.Background(), credentials)
	if err != nil {
		t.Fatalf("RetrieveByCredentials: %v", err)
	}
	if user == nil {
		t.Fatal("RetrieveByCredentials found nobody")
	}
	if user.GetAuthPassword() != hashed {
		t.Fatalf("the user was hydrated with %q, want the stored hash", user.GetAuthPassword())
	}

	if !provider.ValidateCredentials(context.Background(), user, credentials) {
		t.Fatal("the real bcrypt hash did not authenticate the password it was made from")
	}

	credentials["password"] = "correct horse battery stapler"
	if provider.ValidateCredentials(context.Background(), user, credentials) {
		t.Fatal("a wrong password authenticated")
	}
}

// The lookup never mentions the password, in the text or in the bindings. It is
// the array_filter that opens retrieveByCredentials: finding a user BY their
// password is a lookup nobody can index and a comparison no database does in
// constant time.
func TestRetrieveByCredentialsNeverPutsThePasswordInTheStatement(t *testing.T) {
	connection := (&fakeConnection{}).queue(query.Record{"id": int64(7)})
	provider := newEloquentProvider(connection, cheapHasher())

	if _, err := provider.RetrieveByCredentials(context.Background(), map[string]any{
		"email":                 "ana@example.com",
		"password":              "correct horse",
		"password_confirmation": "correct horse",
		"old_password":          "battery staple",
	}); err != nil {
		t.Fatalf("RetrieveByCredentials: %v", err)
	}

	statement := connection.only(t)
	statement.assertScopedBy(t, tenant)
	statement.assertNeverBinds(t, "correct horse")
	statement.assertNeverBinds(t, "battery staple")
	if strings.Contains(statement.sql, "password") {
		t.Fatalf("the lookup names a password column: %s", statement.sql)
	}
}

// Credentials that are nothing but password keys match nobody, and no statement
// is issued -- a query with no where clause would answer with the first user in
// the table.
func TestRetrieveByCredentialsWithOnlyPasswordKeysIssuesNoStatement(t *testing.T) {
	connection := (&fakeConnection{}).queue(query.Record{"id": int64(7)})
	provider := newEloquentProvider(connection, cheapHasher())

	user, err := provider.RetrieveByCredentials(context.Background(), map[string]any{"password": "correct horse"})
	if err != nil {
		t.Fatalf("RetrieveByCredentials: %v", err)
	}
	if user != nil {
		t.Fatalf("credentials with nothing but a password matched %v", user)
	}
	if len(connection.statements) != 0 {
		t.Fatalf("the provider issued %v", connection.statements)
	}
}

// A credential holding a slice is a whereIn, and a callback is handed the query,
// which is the PHP's three shapes.
func TestRetrieveByCredentialsAppliesTheThreeCredentialShapes(t *testing.T) {
	connection := (&fakeConnection{}).queue(query.Record{"id": int64(7)})
	provider := newEloquentProvider(connection, cheapHasher())

	if _, err := provider.RetrieveByCredentials(context.Background(), map[string]any{
		"email": "ana@example.com",
		"role":  []string{"owner", "admin"},
		"scope": func(q *query.Builder) { q.Where("active", "=", true) },
	}); err != nil {
		t.Fatalf("RetrieveByCredentials: %v", err)
	}

	statement := connection.only(t)
	for _, want := range []string{`"email" = ?`, `"role" in (?, ?)`, `"active" = ?`} {
		if !strings.Contains(statement.sql, want) {
			t.Fatalf("the lookup is missing %s: %s", want, statement.sql)
		}
	}
}

// A nil result is "no such user" and not a user with no columns, which is what
// an interface holding a typed nil would have been.
func TestRetrieveByIDFindsNobodyWithoutFailing(t *testing.T) {
	connection := &fakeConnection{}
	provider := newEloquentProvider(connection, cheapHasher())

	user, err := provider.RetrieveByID(context.Background(), 7)
	if err != nil {
		t.Fatalf("RetrieveByID: %v", err)
	}
	if user != nil {
		t.Fatalf("RetrieveByID answered %#v for an empty table, want nil", user)
	}
	connection.only(t).assertScopedBy(t, tenant)
}

// A model that cannot be filled from a row is the one thing this package cannot
// check at compile time, so it is named rather than silently empty.
func TestRetrieveByIDRefusesAModelThatCannotBeHydrated(t *testing.T) {
	connection := (&fakeConnection{}).queue(query.Record{"id": int64(7)})
	provider := users.NewEloquentUserProvider(cheapHasher(), newPlainUser, connection.users, tenant)

	if _, err := provider.RetrieveByID(context.Background(), 7); !errors.Is(err, users.ErrNotHydratable) {
		t.Fatalf("RetrieveByID answered %v, want ErrNotHydratable", err)
	}
}

// ---------------------------------------------------------------------------
// The remember-me token
// ---------------------------------------------------------------------------

func TestRetrieveByTokenComparesTheStoredToken(t *testing.T) {
	for name, test := range map[string]struct {
		stored string
		given  string
		want   bool
	}{
		"the same token":     {"a-remember-token", "a-remember-token", true},
		"a different token":  {"a-remember-token", "another-token", false},
		"never remembered":   {"", "", false},
		"empty cookie value": {"a-remember-token", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			connection := (&fakeConnection{}).queue(query.Record{
				"id":             int64(7),
				"remember_token": test.stored,
			})
			provider := newEloquentProvider(connection, cheapHasher())

			user, err := provider.RetrieveByToken(context.Background(), 7, test.given)
			if err != nil {
				t.Fatalf("RetrieveByToken: %v", err)
			}
			if (user != nil) != test.want {
				t.Fatalf("RetrieveByToken answered %v, want a user: %v", user, test.want)
			}
		})
	}
}

// The write goes to the row AND to the instance the caller holds, because the
// guard queues a cookie from the user it just updated.
func TestUpdateRememberTokenWritesTheColumnAndTheInstance(t *testing.T) {
	connection := &fakeConnection{affected: 1}
	provider := newEloquentProvider(connection, cheapHasher())
	user := &testUser{attributes: map[string]any{"id": int64(7)}}

	if err := provider.UpdateRememberToken(context.Background(), user, "a-new-token"); err != nil {
		t.Fatalf("UpdateRememberToken: %v", err)
	}

	if user.GetRememberToken() != "a-new-token" {
		t.Fatalf("the instance still holds %q", user.GetRememberToken())
	}

	statement := connection.only(t)
	if statement.kind != "update" {
		t.Fatalf("the provider issued a %s, want an update", statement.kind)
	}
	statement.assertScopedBy(t, tenant)
	if strings.Contains(statement.sql, "updated_at") {
		t.Fatalf("being remembered looks like the account was edited: %s", statement.sql)
	}
}

// ---------------------------------------------------------------------------
// rehashPasswordIfRequired
// ---------------------------------------------------------------------------

// A hash written at a lower cost than the one in force is rewritten, in the row
// and on the instance, and the new hash verifies against the same password.
func TestRehashPasswordIfRequiredUpgradesAWeakerHash(t *testing.T) {
	stored := mustHash(t, hasherAt(4), "correct horse battery staple")

	connection := &fakeConnection{affected: 1}
	stronger := hasherAt(6)
	provider := newEloquentProvider(connection, stronger)

	user := &testUser{attributes: map[string]any{"id": int64(7), "password": stored}}
	credentials := map[string]any{"password": "correct horse battery staple"}

	if err := provider.RehashPasswordIfRequired(context.Background(), user, credentials, false); err != nil {
		t.Fatalf("RehashPasswordIfRequired: %v", err)
	}

	rewritten := user.GetAuthPassword()
	if rewritten == stored {
		t.Fatal("the hash was not rewritten")
	}
	if !stronger.Check("correct horse battery staple", rewritten) {
		t.Fatal("the rewritten hash does not verify the password it was made from")
	}
	if stronger.NeedsRehash(rewritten) {
		t.Fatal("the rewritten hash still needs a rehash")
	}

	statement := connection.only(t)
	if statement.kind != "update" {
		t.Fatalf("the provider issued a %s, want an update", statement.kind)
	}
	statement.assertScopedBy(t, tenant)
	statement.assertNeverBinds(t, "correct horse battery staple")
}

// The common case: a hash that already meets the parameters is left alone, and
// no statement is issued on an ordinary sign-in.
func TestRehashPasswordIfRequiredLeavesAMatchingHashAlone(t *testing.T) {
	hasher := hasherAt(4)
	stored := mustHash(t, hasher, "correct horse battery staple")

	connection := &fakeConnection{affected: 1}
	provider := newEloquentProvider(connection, hasher)
	user := &testUser{attributes: map[string]any{"id": int64(7), "password": stored}}

	if err := provider.RehashPasswordIfRequired(context.Background(), user, map[string]any{"password": "correct horse battery staple"}, false); err != nil {
		t.Fatalf("RehashPasswordIfRequired: %v", err)
	}
	if len(connection.statements) != 0 {
		t.Fatalf("an ordinary sign-in issued %v", connection.statements)
	}
	if user.GetAuthPassword() != stored {
		t.Fatal("the hash was rewritten when it did not need to be")
	}

	// force is what LogoutOtherDevices passes, and it rewrites anyway.
	if err := provider.RehashPasswordIfRequired(context.Background(), user, map[string]any{"password": "correct horse battery staple"}, true); err != nil {
		t.Fatalf("RehashPasswordIfRequired(force): %v", err)
	}
	if user.GetAuthPassword() == stored {
		t.Fatal("force did not rewrite the hash")
	}
}

// Rehashing runs right after a sign-in that proved the plain password, so an
// absent one means the caller passed different credentials to the check and to
// the rehash. It is named rather than a nil dereference.
func TestRehashPasswordIfRequiredNeedsThePlainPassword(t *testing.T) {
	connection := &fakeConnection{affected: 1}
	provider := newEloquentProvider(connection, hasherAt(6))
	user := &testUser{attributes: map[string]any{"id": int64(7), "password": mustHash(t, hasherAt(4), "correct horse")}}

	err := provider.RehashPasswordIfRequired(context.Background(), user, map[string]any{"email": "ana@example.com"}, false)
	if !errors.Is(err, users.ErrNoPassword) {
		t.Fatalf("RehashPasswordIfRequired answered %v, want ErrNoPassword", err)
	}
	if len(connection.statements) != 0 {
		t.Fatalf("it wrote %v with no password to hash", connection.statements)
	}
}

// The coercion reaches the rehash too: the same unquoted password that now
// authenticates is the one that gets written back.
func TestRehashPasswordIfRequiredCoercesThePassword(t *testing.T) {
	connection := &fakeConnection{affected: 1}
	stronger := hasherAt(6)
	provider := newEloquentProvider(connection, stronger)
	user := &testUser{attributes: map[string]any{"id": int64(7), "password": mustHash(t, hasherAt(4), "12345")}}

	if err := provider.RehashPasswordIfRequired(context.Background(), user, map[string]any{"password": float64(12345)}, false); err != nil {
		t.Fatalf("RehashPasswordIfRequired: %v", err)
	}
	if !stronger.Check("12345", user.GetAuthPassword()) {
		t.Fatal("the rewritten hash does not verify the password that was sent unquoted")
	}
}

// ---------------------------------------------------------------------------
// The tenant
// ---------------------------------------------------------------------------

// A provider wired with no tenant reaches no row at all. auth.SystemGrant
// answers the zero Grant for an empty tenant, and the zero Grant passes no
// Check -- so the failure is a refusal at the statement, not a query that reads
// every customer's users (RULE 14).
func TestAProviderWithNoTenantCannotReadAnything(t *testing.T) {
	connection := (&fakeConnection{}).queue(query.Record{"id": int64(7)})
	provider := users.NewEloquentUserProvider(cheapHasher(), newTestUser, connection.users, "")

	for name, call := range map[string]func() error{
		"retrieveById": func() error {
			_, err := provider.RetrieveByID(context.Background(), 7)
			return err
		},
		"retrieveByCredentials": func() error {
			_, err := provider.RetrieveByCredentials(context.Background(), map[string]any{"email": "ana@example.com"})
			return err
		},
		"updateRememberToken": func() error {
			return provider.UpdateRememberToken(context.Background(), &testUser{attributes: map[string]any{"id": int64(7)}}, "t")
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, auth.ErrForbidden) {
				t.Fatalf("%s answered %v, want auth.ErrForbidden", name, err)
			}
		})
	}

	if len(connection.statements) != 0 {
		t.Fatalf("an unscoped provider reached the connection: %v", connection.statements)
	}
}

// A tenant that could be read as a separator is refused for the same reason an
// empty one is: it is concatenated into a cache key and a storage path.
func TestAProviderWithATenantThatIsNotOneCannotReadAnything(t *testing.T) {
	connection := &fakeConnection{}
	provider := users.NewEloquentUserProvider(cheapHasher(), newTestUser, connection.users, "acme/reports")

	if _, err := provider.RetrieveByID(context.Background(), 7); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("RetrieveByID answered %v, want auth.ErrForbidden", err)
	}
}

// Every statement a provider issues is under a system grant taken for one of the
// two named actions, which is what `aru doctor` audits by name.
func TestTheProviderActionsAreTheTwoConstants(t *testing.T) {
	if users.RetrieveUser != auth.Action("user.retrieve") {
		t.Fatalf("RetrieveUser is %q", users.RetrieveUser)
	}
	if users.UpdateUser != auth.Action("user.update") {
		t.Fatalf("UpdateUser is %q", users.UpdateUser)
	}
}

// ---------------------------------------------------------------------------
// DatabaseUserProvider
// ---------------------------------------------------------------------------

// The provider with no model: the same behaviour over a GenericUser, including
// the coercion, which is the PHP's shared validateCredentials.
func TestDatabaseUserProviderAuthenticatesWithARealBcryptHasher(t *testing.T) {
	hasher := cheapHasher()
	connection := (&fakeConnection{}).queue(query.Record{
		"id":       int64(7),
		"email":    "ana@example.com",
		"password": mustHash(t, hasher, "12345"),
	})
	provider := users.NewDatabaseUserProvider(connection, hasher, "users", tenant)

	credentials := map[string]any{"email": "ana@example.com", "password": float64(12345)}

	user, err := provider.RetrieveByCredentials(context.Background(), credentials)
	if err != nil {
		t.Fatalf("RetrieveByCredentials: %v", err)
	}
	if user == nil {
		t.Fatal("RetrieveByCredentials found nobody")
	}
	if _, ok := user.(*users.GenericUser); !ok {
		t.Fatalf("the provider answered with %T, want a *users.GenericUser", user)
	}
	if !provider.ValidateCredentials(context.Background(), user, credentials) {
		t.Fatal("the password 12345, sent unquoted, did not authenticate the account it hashed")
	}

	statement := connection.only(t)
	statement.assertScopedBy(t, tenant)
	statement.assertNeverBinds(t, "12345")
}

func TestDatabaseUserProviderRehashUpdatesTheGenericUser(t *testing.T) {
	stored := mustHash(t, hasherAt(4), "correct horse battery staple")

	connection := &fakeConnection{affected: 1}
	stronger := hasherAt(6)
	provider := users.NewDatabaseUserProvider(connection, stronger, "users", tenant)

	user := users.NewGenericUser(map[string]any{"id": int64(7), "password": stored})

	if err := provider.RehashPasswordIfRequired(context.Background(), user, map[string]any{"password": "correct horse battery staple"}, false); err != nil {
		t.Fatalf("RehashPasswordIfRequired: %v", err)
	}
	if user.GetAuthPassword() == stored {
		t.Fatal("the generic user still holds the old hash")
	}
	if !stronger.Check("correct horse battery staple", user.GetAuthPassword()) {
		t.Fatal("the rewritten hash does not verify the password it was made from")
	}
	connection.only(t).assertScopedBy(t, tenant)
}

// A row that is not there is nobody, and it must not become a GenericUser with
// no columns.
func TestDatabaseUserProviderFindsNobodyWithoutFailing(t *testing.T) {
	connection := &fakeConnection{}
	provider := users.NewDatabaseUserProvider(connection, cheapHasher(), "users", tenant)

	user, err := provider.RetrieveByID(context.Background(), 7)
	if err != nil {
		t.Fatalf("RetrieveByID: %v", err)
	}
	if user != nil {
		t.Fatalf("RetrieveByID answered %#v for an empty table, want nil", user)
	}
}

// A password column that is not text reads as empty rather than as its Go
// rendering, so an integer column cannot be signed in to by typing the number.
func TestGenericUserRefusesAPasswordColumnThatIsNotText(t *testing.T) {
	user := users.NewGenericUser(map[string]any{"id": int64(7), "password": 42})

	if user.GetAuthPassword() != "" {
		t.Fatalf("the password column read as %q", user.GetAuthPassword())
	}

	provider := users.NewDatabaseUserProvider(&fakeConnection{}, cheapHasher(), "users", tenant)
	if provider.ValidateCredentials(context.Background(), user, map[string]any{"password": 42}) {
		t.Fatal("an integer password column authenticated")
	}
}
