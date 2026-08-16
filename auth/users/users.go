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
	// under: RetrieveByID, RetrieveByToken and RetrieveByCredentials.
	RetrieveUser auth.Action = "user.retrieve"

	// UpdateUser is the action the two writes are authorized under:
	// UpdateRememberToken and RehashPasswordIfRequired.
	UpdateUser auth.Action = "user.update"
)

// ErrNotHydratable is what a provider answers when the value its model
// constructor returned cannot be filled from a row.
//
// It is the one thing this package cannot check at compile time. The
// constructor is declared as func() auth.Authenticatable because that is the
// contract a guard consumes, and filling a value from a row is not part of that
// contract, so it is asked for by assertion instead. Wire a user type that
// embeds *eloquent.Model[T] -- or write the two methods -- and this never fires.
var ErrNotHydratable = errors.New("users: the model cannot be filled from a row (it does not implement users.Hydratable)")

// ErrNoPassword is what RehashPasswordIfRequired answers when the credentials
// it was handed carry no password to hash.
//
// Rehashing happens right after a sign-in that already proved a plain password,
// so an absent one means the caller passed different credentials to the check
// and to the rehash.
var ErrNoPassword = errors.New("users: the credentials carry no password")

// Hydratable is the one thing a user provider needs of a user type that
// auth.Authenticatable does not declare: filling an instance from one row.
//
// The query builder answers with a query.Record, so the step from row to user
// has to be somewhere, and this is it.
//
// The signature is hesape/database/eloquent's, unchanged, so a user type that
// embeds *eloquent.Model[T] satisfies this by embedding it.
type Hydratable interface {
	// SetRawAttributes writes the row onto the value. sync decides whether the
	// original attributes are synced, and hydration passes true.
	SetRawAttributes(attributes map[string]any, sync bool) error
}

// Fillable is what RehashPasswordIfRequired needs to put the new hash on the
// user it was handed.
//
// It is separate from [Hydratable] because the two do different things:
// SetRawAttributes replaces the row, ForceFill merges into it. Rehashing with
// the first would leave a user holding nothing but a password.
//
// A user type that does not implement it is still rehashed in storage; only the
// instance in memory keeps the old hash, and it is about to be replaced by the
// next read anyway.
type Fillable interface {
	// ForceFill merges the attributes into the value, past any guard on them.
	ForceFill(attributes map[string]any) error
}

// hydrate turns one row into the user type the provider was built with.
//
// A nil record yields a nil user, which is what every retrieve method returns
// when nothing matched -- an interface holding a typed nil would compare
// unequal to nil at the call site, which is the bug this signature exists to
// avoid.
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

// filterCredentials drops every key holding the word "password" before a where
// clause is built from it.
//
// Looking a user up BY their password is a lookup nobody can index and a
// comparison no database does in constant time, so the column never reaches the
// statement. The check is on the key containing the word and not on an exact
// match: password, password_confirmation and old_password all go.
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

// applyCredentials turns each remaining credential into a clause on the query.
//
// There are three shapes. A slice of anything becomes a WhereIn, found by
// reflection, because a caller writing []string{"a","b"} means the same thing
// as []any{"a","b"} and a type switch on []any alone would quietly compile the
// first into a comparison against a slice. A func(*query.Builder) is invoked on
// the query so it can add whatever it likes. Anything else is an equality.
//
// The keys are applied in sorted order. A Go map has none of its own, and the
// clauses are ANDed together -- so the rows returned are the same either way,
// and the statement is the same on every run, which is what makes it readable
// in a log and checkable in a test.
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

// asSlice reads a credential as a list of values to match against: any slice
// is one, except a []byte, which is a single value that happens to be bytes.
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

// tokensMatch compares the remember token stored on the row with the one that
// came in on the cookie, in time that does not depend on how many leading bytes
// agree.
//
// An empty stored token never matches: a user who has never been remembered has
// an empty column, and an empty cookie must not authenticate them.
func tokensMatch(stored, given string) bool {
	if stored == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(given)) == 1
}

// passwordOf reads the plain password out of the credentials.
//
// # Why it is not a string assertion
//
// A credentials map does not hold only strings. A login body of
//
//	{"email": "ana@example.com", "password": 12345}
//
// decodes through encoding/json into map[string]any{"password": float64(12345)},
// so credentials["password"].(string) would fail and the account could never be
// signed in to -- with no message saying why, because ValidateCredentials
// answers bool and the guard reports a refusal identical to a wrong password.
// The value is coerced instead, in [plainPassword].
//
// # And why an empty password is not refused early
//
// An empty string goes to the hasher like any other value and is refused there,
// in the time a bcrypt comparison takes. Returning early would make the empty
// password the one refusal that costs nothing, which is a difference a clock can
// read -- SessionGuard.Attempt hides it inside its timebox, but a provider is
// public and is called directly by anything that authenticates without a guard.
//
// A missing key and a nil value are false.
func passwordOf(credentials map[string]any) (string, bool) {
	return plainPassword(credentials["password"])
}

// plainPassword coerces a credential value into the string the hasher takes.
//
// A number becomes its decimal form -- which is the case that matters, because
// that is what a JSON body's unquoted password decodes to. A bool becomes "1"
// or "". A fmt.Stringer becomes its String. A []byte is text a form parser or a
// driver handed over as bytes, and stringOf in genericuser.go reads a column
// the same way.
//
// Anything else -- a slice, a map, a struct with no String method -- is false,
// for the reason hashing.AuthHasher.Check gives: this contract answers bool and
// has nowhere to put an error, and refusing is the direction that fails safely.
//
// Floats are formatted with the shortest form that round-trips, so 12345.0 is
// "12345" and 1.5 is "1.5". No login body carries one in the exponent range.
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
