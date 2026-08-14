package users

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// The actions the providers in this package authorize their own statements
// under.
//
// A user provider runs at the moment somebody is proving who they are, so there
// is no subject to authorize yet and no Policy that could be asked about one.
// That is what auth.SystemGrant is for, and these are the two names it is asked
// under: reading a user, and writing the two columns a sign-in may rewrite --
// the remember token and the password hash.
//
// They are constants and not configuration. An action a caller could choose
// would be an action a caller could widen, and `aru doctor` audits system grants
// by the name they are taken under.
const (
	// RetrieveUser is the action every read in this package is authorized
	// under: retrieveById, retrieveByToken and retrieveByCredentials.
	RetrieveUser auth.Action = "user.retrieve"

	// UpdateUser is the action the two writes are authorized under:
	// updateRememberToken and rehashPasswordIfRequired.
	UpdateUser auth.Action = "user.update"
)

// ErrNotHydratable is what a provider answers when the value its model
// constructor returned cannot be filled from a row.
//
// It is the one thing this package cannot check at compile time. The
// constructor is declared as func() auth.Authenticatable because that is the
// contract a guard consumes; hydration is Eloquent's, not Authenticatable's, so
// it is asked for by assertion. Wire a user type that embeds *eloquent.Model[T]
// -- or write the two methods -- and this never fires.
var ErrNotHydratable = errors.New("users: the model cannot be filled from a row (it does not implement users.Hydratable)")

// ErrNoPassword is what RehashPasswordIfRequired answers when the credentials
// it was handed carry no password to hash.
//
// The PHP reads $credentials['password'] straight into hasher->make() and lets
// PHP raise on the missing key. A named error is the same event with the cause
// written down: rehashing happens right after a sign-in that already proved a
// plain password, so an absent one means the caller passed different credentials
// to the check and to the rehash.
var ErrNoPassword = errors.New("users: the credentials carry no password")

// Hydratable is the one thing an Eloquent model gives a user provider that
// auth.Authenticatable does not declare: filling an instance from one row.
//
// It answers Illuminate\Database\Eloquent\Model::setRawAttributes, which is what
// Eloquent\Builder::hydrate calls on every record it turns into a model. In PHP
// the provider never names it, because $query->first() comes back as a model
// already; here the query builder answers with a query.Record, and the step from
// row to user has to be somewhere.
//
// The signature is hesape/database/eloquent's, unchanged, so a user type that
// embeds *eloquent.Model[T] satisfies this by embedding it.
type Hydratable interface {
	// SetRawAttributes answers Model::setRawAttributes. sync decides whether the
	// original attributes are synced, and hydration passes true.
	SetRawAttributes(attributes map[string]any, sync bool) error
}

// Fillable is Illuminate\Database\Eloquent\Model::forceFill, which
// rehashPasswordIfRequired calls to put the new hash on the model it was handed.
//
// It is separate from Hydratable because the two do different things:
// setRawAttributes replaces the row, forceFill merges into it. Rehashing with
// the first would leave a user holding nothing but a password.
//
// A user type that does not implement it is still rehashed in storage; only the
// instance in memory keeps the old hash, and it is about to be replaced by the
// next read anyway.
type Fillable interface {
	// ForceFill answers Model::forceFill.
	ForceFill(attributes map[string]any) error
}

// hydrate turns one row into the user type the provider was built with.
//
// It answers the part of Eloquent\Builder::hydrate that a provider depends on.
// A nil record is the PHP's null result and yields a nil user, which is what
// every retrieve method returns when nothing matched -- an interface holding a
// typed nil would compare unequal to nil at the call site, which is the bug this
// signature exists to avoid.
func hydrate(newModel func() auth.Authenticatable, record query.Record) (auth.Authenticatable, error) {
	if record == nil {
		return nil, nil
	}
	user := newModel()
	target, ok := user.(Hydratable)
	if !ok {
		return nil, ErrNotHydratable
	}
	if err := target.SetRawAttributes(record, true); err != nil {
		return nil, err
	}
	return user, nil
}

// filterCredentials answers the array_filter that opens retrieveByCredentials:
// every key holding the word "password" is dropped before a where clause is
// built from it.
//
// Looking a user up BY their password is a lookup nobody can index and a
// comparison no database does in constant time, so the column never reaches the
// statement. The check is on the key and not on an exact match, which is the
// PHP's str_contains: password, password_confirmation and old_password all go.
func filterCredentials(credentials map[string]any) map[string]any {
	out := make(map[string]any, len(credentials))
	for key, value := range credentials {
		if strings.Contains(key, "password") {
			continue
		}
		out[key] = value
	}
	return out
}

// applyCredentials answers the foreach that follows it: each remaining
// credential becomes a clause on the query.
//
// The three shapes are the PHP's three. An array or an Arrayable becomes a
// whereIn -- a slice of anything does here, found by reflection, because a
// caller writing []string{"a","b"} means the same thing as []any{"a","b"} and a
// type switch on []any alone would quietly compile it to a comparison against a
// slice. A Closure becomes func(*query.Builder), invoked on the query so it can
// add whatever it likes. Anything else is an equality.
//
// The keys are applied in sorted order. A PHP array remembers the order it was
// written in and a Go map does not, and the clauses are ANDed together -- so the
// rows returned are the same either way, and the statement is the same on every
// run, which is what makes it readable in a log and checkable in a test.
func applyCredentials(q *query.Builder, credentials map[string]any) {
	for _, key := range slices.Sorted(maps.Keys(credentials)) {
		value := credentials[key]

		if callback, ok := value.(func(*query.Builder)); ok {
			callback(q)
			continue
		}
		if values, ok := asSlice(value); ok {
			q.WhereIn(key, values)
			continue
		}
		q.Where(key, "=", value)
	}
}

// asSlice answers PHP's is_array($value) || $value instanceof Arrayable for a
// Go value: any slice is a list of values to match against, except a []byte,
// which is one value that happens to be bytes.
func asSlice(value any) ([]any, bool) {
	if values, ok := value.([]any); ok {
		return values, true
	}
	if _, ok := value.([]byte); ok {
		return nil, false
	}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// tokensMatch answers hash_equals: the remember token stored on the row and the
// one that came in on the cookie are compared in time that does not depend on
// how many leading bytes agree.
//
// An empty stored token never matches, which is the PHP's `$rememberToken &&`
// -- a user who has never been remembered has an empty column, and an empty
// cookie must not authenticate them.
func tokensMatch(stored, given string) bool {
	if stored == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(given)) == 1
}

// passwordOf reads the plain password out of the credentials, the way
// $credentials['password'] does.
//
// # What was wrong, and the input that proved it
//
// It was a strict type assertion, credentials["password"].(string), and a
// credentials map does not hold only strings. A login body of
//
//	{"email": "ana@example.com", "password": 12345}
//
// decodes through encoding/json into map[string]any{"password": float64(12345)},
// the assertion failed, and the account could never be signed in to -- with no
// message saying why, because validateCredentials answers bool and the guard
// reports a refusal identical to a wrong password.
//
// The PHP has no assertion to fail. validateCredentials tests is_null and hands
// the value to password_verify(string $password, string $hash); Illuminate does
// not declare strict_types, so 12345 is coerced to "12345" and the sign-in
// works. So the coercion is done here, in [plainPassword], for every shape PHP
// would have coerced.
//
// # And the early return that is gone
//
// This also refused an empty password before reaching the hasher. The PHP does
// not: "" goes into password_verify like any other value and is refused there,
// in the time a bcrypt comparison takes. Returning early made the empty password
// the one refusal that costs nothing, which is a difference a clock can read --
// SessionGuard.Attempt hides it inside its timebox, but a provider is public and
// is called directly by anything that authenticates without a guard.
//
// A missing key and a nil value are still false. Those are the PHP's is_null,
// which is the one guard validateCredentials really has.
func passwordOf(credentials map[string]any) (string, bool) {
	return plainPassword(credentials["password"])
}

// plainPassword answers PHP's coercion of $credentials['password'] into the
// string parameter of password_verify.
//
// Every case below is a value PHP renders on the way into that call. A number
// becomes its decimal form -- which is the case that matters, because that is
// what a JSON body's unquoted password decodes to. A bool becomes "1" or "",
// which is what (string)true and (string)false give. A fmt.Stringer is an object
// with __toString. A []byte is text a form parser or a driver handed over as
// bytes, and stringOf in genericuser.go reads a column the same way.
//
// Anything else -- a slice, a map, a struct with no String method -- is false.
// PHP raises a TypeError there and the request ends in a 500; false refuses the
// sign-in instead, for the reason hashing.AuthHasher.Check gives: this contract
// answers bool and has nowhere to put the error, and refusing is the direction
// that fails safely.
//
// Floats are formatted with the shortest form that round-trips, so 12345.0 is
// "12345" and 1.5 is "1.5", which is what PHP prints for both. The two
// renderings part company only in the exponent range, where PHP writes "1.0E+21"
// and this writes the digits out; no login body carries one.
func plainPassword(value any) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", false
	case string:
		return v, true
	case []byte:
		return string(v), true
	case bool:
		if v {
			return "1", true
		}
		return "", true
	case int:
		return strconv.Itoa(v), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case fmt.Stringer:
		return v.String(), true
	default:
		return "", false
	}
}
