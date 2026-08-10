// Package cache is the cache: a Repository over a Store, the locks and the
// rate limiter.
//
// It mirrors Illuminate\Cache, and it is split the same way: Repository is the
// thing an application calls, Store is the thing a backend implements. The
// split is what lets the in-process ArrayStore and the RESP store in
// arandu-io/kv be the same cache from the call site, and it is what makes
// cachetest possible -- one contract suite that every Store passes.
//
// # The Grant is not decoration
//
// Every Repository method takes an auth.Grant and the tenant comes out of it
// (RULE 14). A cache key shared across tenants is a data leak with a fast path,
// and it is the kind that survives review because the query underneath was
// correct. The key is cache:<tenant>:<namespace>:<key>, and a Grant carrying no
// tenant -- or a tenant that could be read as a separator -- is an error, not a
// global bucket.
//
// Locks and rate limits are the documented exceptions (docs/15): a scheduler
// lock covers the whole instance, and rate limiting happens before anybody has
// authenticated, so neither has a Grant to take a tenant from. Locks and
// RateLimiter therefore take a name and a key, not a Grant.
//
// # Every entry expires
//
// Put requires a ttl. Laravel's Cache::forever is refused: an entry with no
// expiry is a second copy of the truth, and the day it diverges nobody knows it
// exists.
//
// # What is not here
//
// No Cache::store()/driver()/extend() and no tags. One cache, one way to reach
// it (RULE 9); a second store is a second Repository built over a second Store,
// wired in bootstrap, not selected by a string at the call site. No file or
// database store: the in-process ArrayStore covers development and a single
// instance, and anything beyond that is the kv adapter.
//
// The files it answers to, in the clone at laravel_illuminate/cache:
//
//	ArrayLock.php           -> ArrayStore, the Locking half
//	ArrayStore.php          -> ArrayStore
//	CacheLock.php           -> Lock
//	HasCacheLock.php        -> Locking
//	Lock.php                -> Lock
//	RateLimiter.php         -> RateLimiter, Limit
//	Repository.php          -> Repository, Get, GetMany, Pull, Remember
//	RetrievesMultipleKeys.php -> Repository.PutMany, GetMany
//	RedisStore.php          -> arandu-io/kv, as a Store
//	RedisLock.php           -> arandu-io/kv, as a Locking
//
// The rest of the component does not arrive: ApcStore, DynamoDbStore,
// FileStore, MemcachedStore, MemoizedStore, FailoverStore and SessionStore are
// backends this collection does not ship; TagSet, TaggableStore, TaggedCache,
// RedisTagSet and RedisTaggedCache are tags; CacheManager and
// CacheServiceProvider are the container (ADR 0001); LuaScripts is refused by
// RULE 11, which is what keeps Dragonfly, Redis, Valkey and KeyDB one product
// to this collection.
package cache
