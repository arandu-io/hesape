package passwords

import (
	"context"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/support"
)

// DatabaseTokenRepository keeps reset tokens in a table, one row per address.
//
// The row holds a hash of the token, never the token. The plain token exists
// once, in the return of Create, on its way into the mail -- so a copy of this
// table is a list of addresses that asked, and not a set of working reset links.
//
// Every statement is taken under auth.SystemGrant, because a reset runs for
// somebody who cannot sign in and there is no subject to authorize. The tenant
// comes from configuration and never from the form, and it filters the reads as
// much as the writes.
type DatabaseTokenRepository struct {
	// connection is where the table is opened.
	connection Connection

	// hasher hashes the token before it is stored and compares it on the way
	// back.
	hasher auth.Hasher

	// table is the name of the table the rows live in.
	table string

	// hashKey is the application key the token is HMACed with.
	hashKey string

	// expires is how long a token is good for. It is a Duration so that the
	// unit is written at every call site.
	expires time.Duration

	// throttle is how long after minting one another may be minted. Zero or
	// less turns the throttle off.
	throttle time.Duration

	// tenant is what every statement this repository issues is scoped by.
	tenant string
}

// Verify at compile time that the repository is the contract the broker holds.
var _ TokenRepository = (*DatabaseTokenRepository)(nil)

// NewDatabaseTokenRepository returns a repository over the named table.
//
// An expires of zero or less becomes [DefaultExpires]. A throttle of zero or
// less turns throttling off. See the type's doc for where tenant must come
// from.
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

// Create mints a token for this address and stores a hash of it.
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

// Exists reports that a record for this address is present, has not expired,
// and hashes to the token that was offered.
//
// All three have to hold, and the expiry is checked before the hash: an expired
// record is refused whatever it holds.
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

// RecentlyCreatedToken reports that this address had a token too recently to be
// given another.
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

// Delete removes this address's record.
func (r *DatabaseTokenRepository) Delete(ctx context.Context, user auth.CanResetPassword) error {
	return r.deleteExisting(ctx, user)
}

// DeleteExpired is the sweep a scheduled task runs, which is housekeeping
// rather than enforcement -- Exists already refuses an expired record.
func (r *DatabaseTokenRepository) DeleteExpired(ctx context.Context) error {
	expiredAt := support.Now().Add(-r.expires)

	_, err := r.getTable(ctx).
		Where("created_at", "<", expiredAt).
		Delete(ctx, r.grant(WriteToken))
	return err
}

// CreateNewToken mints a plain token, without storing anything.
func (r *DatabaseTokenRepository) CreateNewToken() string { return newToken(r.hashKey) }

// GetConnection is the connection the table is opened on.
func (r *DatabaseTokenRepository) GetConnection() Connection { return r.connection }

// GetHasher is the hasher the token is hashed and compared with.
func (r *DatabaseTokenRepository) GetHasher() auth.Hasher { return r.hasher }

// deleteExisting removes whatever record this address already had. The number
// of rows removed is dropped; the error is not.
func (r *DatabaseTokenRepository) deleteExisting(ctx context.Context, user auth.CanResetPassword) error {
	_, err := r.getTable(ctx).
		Where("email", "=", user.GetEmailForPasswordReset()).
		Delete(ctx, r.grant(WriteToken))
	return err
}

// getPayload is the row as it is written.
//
// The token column holds the hash and not the token, which is the whole
// arrangement. There is no tenant column in it: the statement stamps it from the
// Grant, and a value passed here for it would not survive anyway.
func (r *DatabaseTokenRepository) getPayload(email, token string) (map[string]any, error) {
	hashed, err := r.hasher.Make(token)
	if err != nil {
		return nil, err
	}
	return map[string]any{"email": email, "token": hashed, "created_at": support.Now()}, nil
}

// recordFor is the read that opens both Exists and RecentlyCreatedToken.
func (r *DatabaseTokenRepository) recordFor(ctx context.Context, user auth.CanResetPassword) (query.Record, error) {
	return r.getTable(ctx).
		Where("email", "=", user.GetEmailForPasswordReset()).
		First(ctx, r.grant(ReadToken))
}

// getTable opens a builder against the token table.
func (r *DatabaseTokenRepository) getTable(ctx context.Context) *query.Builder {
	return r.connection.Table(ctx, r.table)
}

// grant is the authorization every statement in this file goes through. See the
// type's doc for why it is a system grant and where its tenant comes from.
func (r *DatabaseTokenRepository) grant(action auth.Action) auth.Grant {
	return auth.SystemGrant(action, r.tenant)
}
