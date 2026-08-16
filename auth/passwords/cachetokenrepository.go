package passwords

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/support"
)

// CacheTokenRepository is the same contract as [DatabaseTokenRepository], over
// a cache store instead of a table.
//
// It exists because a reset token is short lived and self-expiring, which is
// what a cache is: no migration, no sweep, and the entry is gone an hour later
// whether or not anybody ran a scheduled task. The trade is that flushing the
// cache invalidates every reset link in flight, which is an inconvenience and
// not an incident.
//
// What is stored is a hash of the token, exactly as in the table -- a cache is
// not a safer place for a secret than a database.
//
// The Grant is the database repository's: a system grant, under an action, with
// a tenant that came from configuration. It is what the cache builds the key
// prefix from, so two tenants asking to reset the same address do not share an
// entry.
type CacheTokenRepository struct {
	// cache is where the entries are written.
	cache *cache.Repository

	// hasher hashes the token before it is stored and compares it on the way
	// back.
	hasher auth.Hasher

	// hashKey is the application key the token is HMACed with.
	hashKey string

	// expires is how long a token is good for, and the ttl each entry is written
	// with.
	expires time.Duration

	// throttle is how long after minting one another may be minted. Zero or
	// less turns the throttle off.
	throttle time.Duration

	// tenant is what every entry this repository writes is keyed under.
	tenant string
}

// Verify at compile time that the repository is the contract the broker holds.
var _ TokenRepository = (*CacheTokenRepository)(nil)

// cachedToken is what goes into the cache: the hash, and when it was minted.
//
// It is a struct because the cache codec is JSON, which reads a struct back as
// the struct and a heterogeneous array as neither element. Nothing has to
// format the timestamp either way: a time.Time goes through JSON and comes back
// a time.Time.
type cachedToken struct {
	// Token is the hash of the token, never the token.
	Token string `json:"token"`

	// CreatedAt is when it was minted, which is what the expiry and the throttle
	// are measured from.
	CreatedAt time.Time `json:"created_at"`
}

// NewCacheTokenRepository returns a repository over a cache store.
//
// An expires of zero or less becomes [DefaultExpires]. A throttle of zero or
// less turns throttling off. See the type's doc for where tenant must come
// from.
func NewCacheTokenRepository(
	store *cache.Repository,
	hasher auth.Hasher,
	hashKey string,
	expires, throttle time.Duration,
	tenant string,
) *CacheTokenRepository {
	return &CacheTokenRepository{
		cache:    store,
		hasher:   hasher,
		hashKey:  hashKey,
		expires:  expiresOr(expires),
		throttle: throttle,
		tenant:   tenant,
	}
}

// Create mints a token for this address and stores a hash of it.
//
// The existing entry is deleted first, so one address has at most one live
// token. The entry is written with the expiry as its ttl, which is what makes
// [CacheTokenRepository.DeleteExpired] unnecessary here.
func (r *CacheTokenRepository) Create(ctx context.Context, user auth.CanResetPassword) (string, error) {
	if err := r.Delete(ctx, user); err != nil {
		return "", err
	}

	token := newToken(r.hashKey)

	hashed, err := r.hasher.Make(token)
	if err != nil {
		return "", err
	}
	record := cachedToken{Token: hashed, CreatedAt: support.Now()}

	if err := r.cache.Put(ctx, r.grant(WriteToken), r.CacheKey(user), record, r.expires); err != nil {
		return "", err
	}
	return token, nil
}

// Exists reports that an entry for this address is present, has not expired,
// and hashes to the token that was offered.
//
// The expiry is checked even though the entry carries a ttl that should have
// removed it. A store whose clock ran slow, or one that keeps an entry past its
// ttl, must not extend the life of a reset link -- the record says when it was
// made, and that is what decides.
func (r *CacheTokenRepository) Exists(ctx context.Context, user auth.CanResetPassword, token string) (bool, error) {
	record, found, err := r.recordFor(ctx, user)
	if err != nil || !found {
		return false, err
	}
	if record.Token == "" || tokenExpired(record.CreatedAt, r.expires) {
		return false, nil
	}
	return r.hasher.Check(token, record.Token), nil
}

// RecentlyCreatedToken reports that this address had a token too recently to be
// given another.
func (r *CacheTokenRepository) RecentlyCreatedToken(ctx context.Context, user auth.CanResetPassword) (bool, error) {
	record, found, err := r.recordFor(ctx, user)
	if err != nil || !found {
		return false, err
	}
	if record.Token == "" {
		return false, nil
	}
	return tokenRecentlyCreated(record.CreatedAt, r.throttle), nil
}

// Delete removes this address's entry.
func (r *CacheTokenRepository) Delete(ctx context.Context, user auth.CanResetPassword) error {
	return r.cache.Forget(ctx, r.grant(WriteToken), r.CacheKey(user))
}

// DeleteExpired does nothing: the store expires the entries itself, which is
// the reason to use this repository at all.
func (r *CacheTokenRepository) DeleteExpired(context.Context) error { return nil }

// CacheKey is the address, hashed with SHA-256.
//
// It is hashed and not used as it stands because a cache key travels further
// than a row does -- into a shared store, into a slow-command log, into whatever
// the operator has attached to it -- and an address is personal data.
func (r *CacheTokenRepository) CacheKey(user auth.CanResetPassword) string {
	sum := sha256.Sum256([]byte(user.GetEmailForPasswordReset()))
	return hex.EncodeToString(sum[:])
}

// recordFor is the read that opens both Exists and RecentlyCreatedToken.
//
// A miss is reported as found=false rather than as an error, because neither
// caller treats "no entry" as a failure. Anything else is a store that did not
// answer, which is a failure and is not a reason to say "no token".
func (r *CacheTokenRepository) recordFor(ctx context.Context, user auth.CanResetPassword) (cachedToken, bool, error) {
	record, err := cache.Get[cachedToken](ctx, r.cache, r.grant(ReadToken), r.CacheKey(user))
	switch {
	case err == nil:
		return record, true, nil
	case errors.Is(err, cache.ErrNotFound):
		return cachedToken{}, false, nil
	default:
		return cachedToken{}, false, err
	}
}

// grant is the authorization every cache call in this file goes through.
func (r *CacheTokenRepository) grant(action auth.Action) auth.Grant {
	return auth.SystemGrant(action, r.tenant)
}
