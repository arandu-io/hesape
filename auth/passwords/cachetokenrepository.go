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

// CacheTokenRepository answers
// Illuminate\Auth\Passwords\CacheTokenRepository: the same contract over a
// cache store instead of a table.
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
// entry (RULE 14).
type CacheTokenRepository struct {
	// cache answers $cache.
	cache *cache.Repository

	// hasher answers $hasher.
	hasher auth.Hasher

	// hashKey answers $hashKey.
	hashKey string

	// expires answers $expires, as a Duration for the reason the database
	// repository's is one.
	expires time.Duration

	// throttle answers $throttle. Zero or less turns the throttle off.
	throttle time.Duration

	// tenant is the tenant every entry this repository writes is keyed under.
	tenant string
}

// Verify at compile time that the repository is the contract the broker holds.
var _ TokenRepository = (*CacheTokenRepository)(nil)

// cachedToken is what the PHP puts in the cache as a two element array:
// [$this->hasher->make($token), Carbon::now()->format($this->format)].
//
// It is a struct here because the cache codec is JSON, which reads a struct back
// as the struct and a heterogeneous array as neither element. That is also what
// removes CacheTokenRepository::$format: the property exists to flatten a Carbon
// into a string and to parse it again, and a time.Time goes through JSON and
// comes back a time.Time.
type cachedToken struct {
	// Token is the hash of the token, never the token.
	Token string `json:"token"`

	// CreatedAt is when it was minted, which is what the expiry and the throttle
	// are measured from.
	CreatedAt time.Time `json:"created_at"`
}

// NewCacheTokenRepository answers CacheTokenRepository::__construct.
//
// expires of zero or less is the PHP's default argument, one hour. throttle of
// zero or less turns throttling off. tenant has no counterpart in the PHP; see
// the type's doc.
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

// Create answers CacheTokenRepository::create.
//
// The existing entry is deleted first, as in the table, so one address has at
// most one live token. The entry is written with the expiry as its ttl, which is
// what makes deleteExpired unnecessary here.
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

// Exists answers CacheTokenRepository::exists.
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

// RecentlyCreatedToken answers CacheTokenRepository::recentlyCreatedToken.
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

// Delete answers CacheTokenRepository::delete.
func (r *CacheTokenRepository) Delete(ctx context.Context, user auth.CanResetPassword) error {
	return r.cache.Forget(ctx, r.grant(WriteToken), r.CacheKey(user))
}

// DeleteExpired answers CacheTokenRepository::deleteExpired, whose body in the
// PHP is empty and whose body is empty here: the store expires the entries
// itself, which is the reason to use this repository at all.
func (r *CacheTokenRepository) DeleteExpired(context.Context) error { return nil }

// CacheKey answers CacheTokenRepository::cacheKey: the address, hashed.
//
// It is hashed and not used as it stands because a cache key travels further
// than a row does -- into a shared store, into a slow-command log, into whatever
// the operator has attached to it -- and an address is personal data. sha256 is
// the PHP's hash('sha256', ...).
func (r *CacheTokenRepository) CacheKey(user auth.CanResetPassword) string {
	sum := sha256.Sum256([]byte(user.GetEmailForPasswordReset()))
	return hex.EncodeToString(sum[:])
}

// recordFor is the read that opens exists and recentlyCreatedToken.
//
// A miss is reported as found=false rather than as an error, because the PHP
// reads null out of the cache for it and neither caller treats it as a failure.
// Anything else is a store that did not answer, which is a failure and is not a
// reason to say "no token".
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
