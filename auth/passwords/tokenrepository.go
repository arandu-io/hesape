package passwords

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/str"
	"github.com/arandu-io/hesape/support"
)

// The actions the token repositories in this package authorize their own
// statements under.
//
// A password reset runs for somebody who cannot sign in, so there is no subject
// and no Policy to ask about one. That is what auth.SystemGrant is for, and the
// tenant it carries is the repository's, from configuration -- never from the
// address that arrived in the form (RULE 14). Reads are scoped by it exactly as
// writes are (RULE 17): a token lookup that is not filtered by tenant would
// accept a token minted for another customer's account with the same address.
const (
	// ReadToken is the action the token lookups are authorized under: exists and
	// recentlyCreatedToken.
	ReadToken auth.Action = "password.read"

	// WriteToken is the action the token writes are authorized under: create,
	// delete and deleteExpired.
	WriteToken auth.Action = "password.write"
)

// DefaultExpires is the PHP's default $expires: a reset token is good for an
// hour.
const DefaultExpires = time.Hour

// TokenRepository answers
// Illuminate\Auth\Passwords\TokenRepositoryInterface: where reset tokens are
// kept, and the only thing the broker knows about them.
//
// The token itself is never stored. What is stored is a hash of it, so that a
// dump of the table -- or of the cache -- does not hand somebody a working reset
// link for every account that asked for one in the last hour.
//
// Every method takes a context, which is the fifth mechanical change of ADR
// 0044: each of them reaches storage.
type TokenRepository interface {
	// Create answers TokenRepositoryInterface::create: it mints a token,
	// replaces whatever the user had, and returns the plain token -- the only
	// moment it exists.
	Create(ctx context.Context, user auth.CanResetPassword) (string, error)

	// Exists answers TokenRepositoryInterface::exists: a token record is
	// present, unexpired, and matches.
	Exists(ctx context.Context, user auth.CanResetPassword, token string) (bool, error)

	// RecentlyCreatedToken answers
	// TokenRepositoryInterface::recentlyCreatedToken: the throttle that stops a
	// form from mailing somebody once a second.
	RecentlyCreatedToken(ctx context.Context, user auth.CanResetPassword) (bool, error)

	// Delete answers TokenRepositoryInterface::delete.
	Delete(ctx context.Context, user auth.CanResetPassword) error

	// DeleteExpired answers TokenRepositoryInterface::deleteExpired.
	DeleteExpired(ctx context.Context) error
}

// Connection is the part of Illuminate\Database\ConnectionInterface that a
// token repository asks for: a query builder against one table.
//
// It is declared here, with its consumer, rather than imported from the
// database package -- the same choice query.Connection makes, and for the same
// reason. *database.Connection satisfies it as written.
type Connection interface {
	// Table answers ConnectionInterface::table. The context is the fifth
	// mechanical change of ADR 0044: a hesape builder binds its connection at
	// the moment it is made, so the context enters here.
	Table(ctx context.Context, table any, as ...string) *query.Builder
}

// newToken answers the hash_hmac('sha256', Str::random(40), $this->hashKey)
// that both repositories mint their tokens with.
//
// It is one function rather than a method on each, because the PHP writes the
// same expression in DatabaseTokenRepository::createNewToken and inline in
// CacheTokenRepository::create -- and two spellings of how a reset token is
// generated is one spelling too many.
//
// The randomness is Str::random's, which is the system source. The HMAC over it
// adds nothing to the entropy and is kept because the PHP keeps it: it makes the
// token a fixed-width hex string, and it means a token from one application's
// key is not a token in another's.
func newToken(hashKey string) string {
	mac := hmac.New(sha256.New, []byte(hashKey))
	mac.Write([]byte(str.Random(40)))
	return hex.EncodeToString(mac.Sum(nil))
}

// tokenExpired answers the protected tokenExpired of both repositories:
// createdAt plus $expires is in the past.
func tokenExpired(createdAt time.Time, expires time.Duration) bool {
	return createdAt.Add(expires).Before(support.Now())
}

// tokenRecentlyCreated answers the protected tokenRecentlyCreated of both
// repositories: createdAt plus $throttle is still in the future.
//
// A throttle of zero or less turns it off, which is the PHP's first line and is
// what the configuration means by omitting it.
func tokenRecentlyCreated(createdAt time.Time, throttle time.Duration) bool {
	if throttle <= 0 {
		return false
	}
	return createdAt.Add(throttle).After(support.Now())
}

// expiresOr answers the PHP's default argument of 3600 seconds, which Go has no
// syntax for: a repository built with no expiry is built with the default one.
func expiresOr(expires time.Duration) time.Duration {
	if expires <= 0 {
		return DefaultExpires
	}
	return expires
}

// stringOf reads a stored column or cached field that ought to be text.
//
// A driver may hand a text column back as []byte -- MySQL does -- and PHP would
// have coerced it on the way into the comparison. Anything else is not text and
// reads as empty, which no hash matches.
func stringOf(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}

// timeOf reads a stored created_at, which arrives as whatever the driver made
// of it.
//
// It answers Carbon::parse, narrowed to what a timestamp column can be: the
// driver's own time.Time, or the two spellings a driver that hands back text
// uses. The PHP throws on anything else; here an unreadable value reports false,
// and both callers treat that as "this record cannot be trusted" -- which fails
// closed, refusing a token rather than accepting one whose age is unknown.
func timeOf(value any) (time.Time, bool) {
	switch v := value.(type) {
	case time.Time:
		return v, true
	case string:
		return parseTime(v)
	case []byte:
		return parseTime(string(v))
	default:
		return time.Time{}, false
	}
}

// storedTimeLayouts are the layouts a timestamp column comes back as when the
// driver hands over text: the SQL spelling every engine writes, and RFC 3339 for
// a driver that renders a zone.
var storedTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999",
	"2006-01-02 15:04:05",
}

func parseTime(value string) (time.Time, bool) {
	for _, layout := range storedTimeLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
