// Package cache is the cache: a Repository over a Store, the stores, the locks,
// the tags and the rate limiter.
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
// Put requires a ttl. Laravel's Cache::forever is here and is written down as a
// century rather than as an absence, because an entry with no expiry is a second
// copy of the truth and the day it diverges nobody knows it exists. Reach for
// Forever rarely.
//
// A ttl that has already passed is not an entry: Put and PutMany forget the
// keys, and Add reports that it wrote nothing, which is what Repository::put,
// Repository::putMany and Repository::add each do with "$seconds <= 0".
// Increment is the exception, and its doc comment says why.
//
// # The stores
//
// ArrayStore is in-process and is the default. FileStore is on disk, shared
// between the processes of one machine. DatabaseStore is a table, and is the
// only one whose locks survive the cache being emptied. NullStore keeps nothing.
// MemoizedStore remembers, for one request, what another store already
// answered. FailoverStore tries several in turn.
//
// The RESP store -- Dragonfly, Redis, Valkey, KeyDB, which are one product to
// this collection (RULE 11) -- is in arandu-io/kv and arrives through
// CacheManager.Extend, so that the driver is in the binaries that use it and no
// others.
//
// # What is not here
//
// No Memcached, no APC and no DynamoDB: three backends this collection does not
// carry. No SessionStore: a cache whose lifetime is a session is a session, and
// hesape/session is where that lives. No LuaScripts, which RULE 11 refuses --
// it is what keeps Dragonfly, Redis, Valkey and KeyDB one product to this
// collection.
//
// The files it answers to, in the clone at laravel_illuminate/cache -- which is
// the source this package was written from, with
// reference_laravel/framework/src/Illuminate/Cache as the second reading:
//
//	ArrayLock.php             -> ArrayStore, the Locking half
//	ArrayStore.php            -> ArrayStore
//	CacheLock.php             -> Lock
//	CacheManager.php          -> CacheManager, Config, StoreConfig
//	Console/                  -> cache/console
//	DatabaseLock.php          -> DatabaseStore, the Locking half
//	DatabaseStore.php         -> DatabaseStore
//	Events/                   -> cache/events
//	FailoverStore.php         -> FailoverStore
//	FileLock.php              -> FileStore, the Locking half
//	FileStore.php             -> FileStore, Filesystem, LocalFilesystem
//	HasCacheLock.php          -> Locking
//	Lock.php                  -> Lock, Locks
//	MemoizedStore.php         -> MemoizedStore
//	NoLock.php                -> NoLock
//	NullStore.php             -> NullStore
//	RateLimiter.php           -> RateLimiter
//	RateLimiting/Limit.php    -> Limit
//	Repository.php            -> Repository, Get, Many, Pull, Remember, Flexible
//	RetrievesMultipleKeys.php -> the Many and PutMany of every store here
//	TagSet.php                -> TagSet
//	TaggableStore.php         -> Taggable
//	TaggedCache.php           -> TaggedCache
//
// The rest does not arrive, and each has a reason. ApcStore, ApcWrapper,
// DynamoDbStore, DynamoDbLock, MemcachedStore, MemcachedLock,
// MemcachedConnector and PhpRedisLock are backends this collection does not
// carry. RedisStore, RedisLock, RedisTagSet and RedisTaggedCache are in
// arandu-io/kv, with the driver they need. SessionStore belongs to
// hesape/session. LuaScripts is RULE 11. CacheServiceProvider is the container
// (ADR 0001), and so is CacheManager::setApplication.
//
// Method by method, so that the list can be checked rather than believed. Each
// one with the ADR 0056 reason number:
//
//	RedisStore::connection, ::lockConnection, ::getRedis, RedisTagSet::addEntry,
//	    ::entries, ::flushStaleEntries and RedisTaggedCache::flushStale --
//	    reason 3, all seven. They are the RESP store, which is arandu-io/kv and
//	    arrives through [CacheManager.Extend]. connection() and lockConnection()
//	    also answer a second connection name for locks, which RULE 11 has no
//	    room for: one product, one connection.
//	MemcachedConnector::connect, ::getMemcached, MemcachedStore::getMemcached
//	    and DynamoDbStore::getClient -- reason 3: two backends this collection
//	    does not carry, and the driver handle each of them hands back.
//	Console\ClearCommand::flushFacades -- reason 2: it calls Facade::clearResolvedInstance
//	    for the bindings the command touched, and there are no facades
//	    (ADR 0002). What it exists to fix -- a stale store held from before the
//	    flush -- cannot happen when the store is the value the caller already
//	    holds.
//	Repository::offsetExists, ::offsetGet, ::offsetSet and ::offsetUnset --
//	    reason 1: ArrayAccess is how PHP writes $cache['key']. Go has no
//	    operator to overload; the four methods are [Repository.Has],
//	    [Repository.Get], [Repository.Put] and [Repository.Forget], which is
//	    what each of them forwards to -- and every one of those takes an
//	    [auth.Grant], which a bracket has nowhere to put (RULE 14).
//	CacheServiceProvider::register, ::provides and CacheManager::setApplication
//	    -- reason 2: they bind 'cache', 'cache.store' and 'memcached.connector'
//	    into the container and name them again for the deferred provider.
//	    [NewCacheManager] takes the configuration and the stores instead.
package cache
