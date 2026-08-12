package passwords

import (
	"context"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/support"
)

// DatabaseTokenRepository answers
// Illuminate\Auth\Passwords\DatabaseTokenRepository: reset tokens kept in a
// table, one row per address.
//
// The row holds a hash of the token, never the token. The plain token exists
// once, in the return of Create, on its way into the mail -- so a copy of this
// table is a list of addresses that asked, and not a set of working reset links.
//
// Every statement is taken under auth.SystemGrant, because a reset runs for
// somebody who cannot sign in and there is no subject to authorize. The tenant
// comes from configuration and never from the form (RULE 14), and it filters the
// reads as much as the writes (RULE 17).
type DatabaseTokenRepository struct {
	// connection answers $connection.
	connection Connection

	// hasher answers $hasher. It hashes the token before it is stored and
	// compares it on the way back.
	hasher auth.Hasher

	// table answers $table.
	table string

	// hashKey answers $hashKey: the application key the token is HMACed with.
	hashKey string

	// expires answers $expires. The PHP counts seconds; a Duration says which
	// unit it is at every call site, which is the point of the type.
	expires time.Duration

	// throttle answers $throttle. Zero or less turns the throttle off, which is
	// what the PHP's body does with it.
	throttle time.Duration

	// tenant is the tenant every statement this repository issues is scoped by.
	tenant string
}

// Verify at compile time that the repository is the contract the broker holds.
var _ TokenRepository = (*DatabaseTokenRepository)(nil)

// NewDatabaseTokenRepository answers DatabaseTokenRepository::__construct.
//
// expires of zero or less is the PHP's default argument, one hour, which Go has
// no syntax for. throttle of zero or less turns throttling off, which is what
// the PHP's tokenRecentlyCreated does with it. tenant has no counterpart in the
// PHP, which has no tenants; see the type's doc for where it must come from.
func NewDatabaseTokenRepository(
	connection Connection,
	hasher auth.Hasher,
	table, hashKey string,
	expires, throttle time.Duration,
	tenant string,
) *DatabaseTokenRepository {
	return &DatabaseTokenRepository{
		connection: connection,
		hasher:     hasher,
		table:      table,
		hashKey:    hashKey,
		expires:    expiresOr(expires),
		throttle:   throttle,
		tenant:     tenant,
	}
}

// Create answers DatabaseTokenRepository::create.
//
// The existing record is deleted first, so one address has at most one live
// token: a second reset request invalidates the link from the first. The plain
// token is returned and is not stored anywhere.
func (r *DatabaseTokenRepository) Create(ctx context.Context, user auth.CanResetPassword) (string, error) {
	email := user.GetEmailForPasswordReset()

	if err := r.deleteExisting(ctx, user); err != nil {
		return "", err
	}

	token := r.CreateNewToken()

	payload, err := r.getPayload(email, token)
	if err != nil {
		return "", err
	}
	if _, err := r.getTable(ctx).Insert(ctx, r.grant(WriteToken), payload); err != nil {
		return "", err
	}
	return token, nil
}

// Exists answers DatabaseTokenRepository::exists: a record for this address is
// present, has not expired, and hashes to the token that was offered.
//
// All three have to hold. The order is the PHP's, and the expiry is checked
// before the hash rather than after: an expired record is refused whatever it
// holds.
func (r *DatabaseTokenRepository) Exists(ctx context.Context, user auth.CanResetPassword, token string) (bool, error) {
	record, err := r.recordFor(ctx, user)
	if err != nil || record == nil {
		return false, err
	}
	createdAt, ok := timeOf(record["created_at"])
	if !ok || tokenExpired(createdAt, r.expires) {
		return false, nil
	}
	return r.hasher.Check(token, stringOf(record["token"])), nil
}

// RecentlyCreatedToken answers
// DatabaseTokenRepository::recentlyCreatedToken.
//
// It is what makes the reset form safe to leave open: without it, every submit
// mints a token and sends a mail, and the form is a way to have somebody else's
// inbox filled from an anonymous endpoint.
func (r *DatabaseTokenRepository) RecentlyCreatedToken(ctx context.Context, user auth.CanResetPassword) (bool, error) {
	record, err := r.recordFor(ctx, user)
	if err != nil || record == nil {
		return false, err
	}
	createdAt, ok := timeOf(record["created_at"])
	if !ok {
		return false, nil
	}
	return tokenRecentlyCreated(createdAt, r.throttle), nil
}

// Delete answers DatabaseTokenRepository::delete.
func (r *DatabaseTokenRepository) Delete(ctx context.Context, user auth.CanResetPassword) error {
	return r.deleteExisting(ctx, user)
}

// DeleteExpired answers DatabaseTokenRepository::deleteExpired: the sweep a
// scheduled task runs, which is housekeeping rather than enforcement -- Exists
// already refuses an expired record.
func (r *DatabaseTokenRepository) DeleteExpired(ctx context.Context) error {
	expiredAt := support.Now().Add(-r.expires)

	_, err := r.getTable(ctx).
		Where("created_at", "<", expiredAt).
		Delete(ctx, r.grant(WriteToken))
	return err
}

// CreateNewToken answers DatabaseTokenRepository::createNewToken.
func (r *DatabaseTokenRepository) CreateNewToken() string { return newToken(r.hashKey) }

// GetConnection answers DatabaseTokenRepository::getConnection.
func (r *DatabaseTokenRepository) GetConnection() Connection { return r.connection }

// GetHasher answers DatabaseTokenRepository::getHasher.
func (r *DatabaseTokenRepository) GetHasher() auth.Hasher { return r.hasher }

// deleteExisting answers the protected
// DatabaseTokenRepository::deleteExisting.
//
// The PHP answers with the number of rows removed and no caller reads it; the
// count is dropped here and the error is not.
func (r *DatabaseTokenRepository) deleteExisting(ctx context.Context, user auth.CanResetPassword) error {
	_, err := r.getTable(ctx).
		Where("email", "=", user.GetEmailForPasswordReset()).
		Delete(ctx, r.grant(WriteToken))
	return err
}

// getPayload answers the protected DatabaseTokenRepository::getPayload: the row
// as it is written.
//
// The token column holds the hash and not the token, which is the whole
// arrangement. There is no tenant column in it: the statement stamps it from the
// Grant, and a value passed here for it would not survive anyway (RULE 14).
func (r *DatabaseTokenRepository) getPayload(email, token string) (map[string]any, error) {
	hashed, err := r.hasher.Make(token)
	if err != nil {
		return nil, err
	}
	return map[string]any{"email": email, "token": hashed, "created_at": support.Now()}, nil
}

// recordFor is the read that opens exists and recentlyCreatedToken, which the
// PHP writes out twice.
func (r *DatabaseTokenRepository) recordFor(ctx context.Context, user auth.CanResetPassword) (query.Record, error) {
	return r.getTable(ctx).
		Where("email", "=", user.GetEmailForPasswordReset()).
		First(ctx, r.grant(ReadToken))
}

// getTable answers the protected DatabaseTokenRepository::getTable.
func (r *DatabaseTokenRepository) getTable(ctx context.Context) *query.Builder {
	return r.connection.Table(ctx, r.table)
}

// grant is the authorization every statement in this file goes through. See the
// type's doc for why it is a system grant and where its tenant comes from.
func (r *DatabaseTokenRepository) grant(action auth.Action) auth.Grant {
	return auth.SystemGrant(action, r.tenant)
}
