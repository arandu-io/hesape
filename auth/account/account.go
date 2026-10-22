package account

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/events"
)

// Account is what this package reads and writes on the application's user
// model. The application's *User implements it; everything else about the user
// -- its roles, its other columns, its JSON -- stays the application's.
//
// model.Entity, SetRawAttributes, ForceFill and Save are what embedding
// model.Model already gives the user type, so the six readers are the whole of
// what an application writes for it.
type Account interface {
	model.Entity

	// AccountID is the primary key, the id column.
	AccountID() string
	// AccountTenant is the tenant the account belongs to, the table's tenant
	// column.
	AccountTenant() string
	// AccountEmail is the stored address, the email column.
	AccountEmail() string
	// AccountName is the name the account is shown by, the name column.
	AccountName() string
	// PasswordHash is the stored password hash, the password column.
	PasswordHash() string
	// IsVerified reports whether the verified_at column holds a time.
	IsVerified() bool

	// SetRawAttributes replaces the row the value holds. A row read by the
	// sign-in provider becomes an account through it.
	SetRawAttributes(attributes map[string]any, sync bool) error
	// ForceFill merges columns into the value. A new account is filled through
	// it, and so is an account a conditional write just changed, so the value
	// handed back is the row that was stored.
	ForceFill(attributes map[string]any) error
	// Save inserts a new account under the Grant it is given.
	Save(ctx context.Context, g auth.Grant) (bool, error)
}

// The columns this package writes on the users table, by name.
//
// They are the columns of the users table every application starts with: a
// conditional update names the column it compares, and a column renamed by an
// application is a column these statements would silently never match.
const (
	columnID         = "id"
	columnTenant     = "tenant_id"
	columnName       = "name"
	columnEmail      = "email"
	columnPassword   = "password"
	columnVerifiedAt = "verified_at"
)

// NormalizeEmail is the one normalization every read and write of an address
// goes through: trimmed and lowercased. An address compared without it is an
// address registered twice.
func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// Fingerprint identifies a complete password hash without exposing it: the
// unpadded base64url SHA-256 of the hash.
//
// A reset link and a pending sign-in carry it, so either is spent by the next
// password change: a new hash has a new fingerprint. It is a function of the
// hash and not a method the application writes, because a link minted by one
// spelling and checked by another is a link that never verifies.
func Fingerprint(passwordHash string) string {
	sum := sha256.Sum256([]byte(passwordHash))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Registration is what a person typed to create an account.
//
// Extra carries what the application's form adds beyond the three fields --
// a document number, a phone -- for Hooks.Prepare to read; this package never
// looks at it.
type Registration struct {
	Name     string
	Email    string
	Password string
	Extra    any
}

// Draft is an account an operator, a seeder or an import creates, with no
// person at a form behind it.
//
// Exactly one of Password and PasswordHash is set. PasswordHash is a stored
// hash carried over from another system; it is accepted only when the hashing
// package can read it, and it is rehashed on the first sign-in. Verified
// creates the account with its address already confirmed. Fill, when set,
// writes the application's own columns -- roles, a document -- before the row
// is saved.
type Draft struct {
	Name         string
	Email        string
	Password     string
	PasswordHash string
	Verified     bool
	Fill         func(Account) error
}

// LogValue says which fields arrived and nothing they carried: the name and
// the address are account data, the password is the credential, and Extra is
// the application's.
func (r Registration) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("name_supplied", r.Name != ""),
		slog.Bool("email_supplied", r.Email != ""),
		slog.Bool("password_supplied", r.Password != ""),
		slog.Bool("extra_supplied", r.Extra != nil),
	)
}

// Format prints what LogValue says, under every verb, %#v included, so a
// registration handed to fmt or to an error message never prints the
// password.
func (r Registration) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "account.Registration{name_supplied:%t email_supplied:%t password_supplied:%t extra_supplied:%t}",
		r.Name != "", r.Email != "", r.Password != "", r.Extra != nil)
}

// LogValue says which fields a draft carries and nothing they hold: the
// password and the hash are credentials.
func (d Draft) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("name_supplied", d.Name != ""),
		slog.Bool("email_supplied", d.Email != ""),
		slog.Bool("password_supplied", d.Password != ""),
		slog.Bool("password_hash_supplied", d.PasswordHash != ""),
		slog.Bool("verified", d.Verified),
		slog.Bool("fill_supplied", d.Fill != nil),
	)
}

// Format prints what LogValue says, under every verb, %#v included.
func (d Draft) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "account.Draft{name_supplied:%t email_supplied:%t password_supplied:%t password_hash_supplied:%t verified:%t fill_supplied:%t}",
		d.Name != "", d.Email != "", d.Password != "", d.PasswordHash != "", d.Verified, d.Fill != nil)
}

// Hooks are the points where an application adds its own rule to what this
// package does. Every one is optional.
type Hooks struct {
	// Admit decides, after the password proved right, whether the account may
	// sign in -- a suspension, a closed account. A refusal is answered as
	// ErrAccountRefused, which matches auth.ErrInvalidCredentials, so the form
	// says what it says for a wrong password and a suspension is not a
	// password oracle. ResetPassword refuses the same accounts as a spent link.
	Admit func(ctx context.Context, user Account) error

	// AdmitClears says a sign-in Admit refused still clears the throttle, as
	// a right password does. Left false, the refusal counts as a failure.
	//
	// Setting it gives back part of what answering a refusal as a wrong
	// password protects. The answer is the same, but the throttle is not:
	// wrong passwords for a refused account run into the lockout, and the
	// right one resets the count. Whoever guesses at a suspended account can
	// therefore tell the right password by the lockout that does not come,
	// and use it the day the account is admitted again. Leave it false unless
	// a refused person locking themselves out is the larger harm.
	AdmitClears bool

	// Prepare runs first in Register, with the registration and the unsaved
	// account. It may refuse, change the registration -- give it a name when
	// the form has none -- and fill the application's own columns.
	Prepare func(ctx context.Context, registration *Registration, user Account) error

	// AfterCreate runs in the transaction that inserted the account, after the
	// insert and before its event is stored, with the Grant the insert was
	// made under. An error rolls back the account and the event with it.
	AfterCreate func(ctx context.Context, grant auth.Grant, user Account) error

	// AfterRegister runs in Register only, in the same transaction, right after
	// AfterCreate and before the event is stored, with the Registration as
	// Prepare left it -- Extra included -- so what the form carried beyond the
	// account's columns reaches the step that stores it without a field on the
	// user model to ferry it. Password is cleared in the copy it receives. An
	// error rolls back the account and the event with it. Provision does not
	// call it: a Draft is all a provisioned account has.
	AfterRegister func(ctx context.Context, grant auth.Grant, user Account, registration Registration) error

	// CloseFields names the application's own columns Close writes beside the
	// ones it clears itself, and the values they get.
	CloseFields func(user Account) map[string]any

	// AfterClose runs in the transaction that closed the account, after its
	// columns are written and before its event is stored, with the Grant the
	// close was made under: where the application revokes what still acts as
	// the account without its password -- personal access tokens, sessions,
	// remembered devices. An error rolls back the close and the event with it.
	AfterClose func(ctx context.Context, grant auth.Grant, user Account) error

	// AfterResetPassword runs in the transaction that replaced the password
	// of a reset -- ResetPassword, from a link, and SetPassword, from an
	// operator's command -- after the new hash is written and before
	// PasswordReset is stored, with the Grant the reset was made under and the
	// account holding the new hash: where the application revokes what still
	// acts as the account without the password -- personal access tokens,
	// remembered devices. A reset is how somebody takes an account back, and a
	// token minted by whoever had it outlives the old password otherwise. An
	// error rolls back the reset and the event with it. A reset that is
	// refused never calls it, and ChangePassword, which proves the current
	// password, does not either.
	AfterResetPassword func(ctx context.Context, grant auth.Grant, user Account) error

	// Payload replaces UserEvent as the payload of the events Users stores.
	Payload func(user Account) any
}

// Config is what NewUsers is built from.
type Config struct {
	// DB is the database the users table and the outbox live in.
	DB *database.DB
	// Outbox stores the account's events. Nil is an outbox over DB.
	Outbox *events.Outbox
	// Query opens a query on the users table: the application's
	// models.Users(db).Base(). It is a function because a builder holds the
	// clauses added to it, and every statement here starts from none.
	Query func() *model.Builder
	// Policy is the application's user policy, adapted by PolicyOf. Every
	// operation a signed-in subject makes on an account is authorized by it.
	Policy auth.Policy[Account]
	// Throttle counts wrong passwords. Nil is auth.NewMemoryThrottle(), which
	// is right for one process; behind more than one, pass the shared one.
	Throttle auth.SignInThrottle
	// Hooks are the application's own rules.
	Hooks Hooks
}
