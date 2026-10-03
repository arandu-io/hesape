// Package cache is the cache: a Repository over a Store, the stores, the locks,
// the tags and the rate limiter.
//
// It is split in two. Repository is the thing an application calls; Store is the
// thing a backend implements. The split is what lets the in-process ArrayStore
// and the RESP store in hesape/redis be the same cache from the call site, and
// it is what makes cachetest possible -- one contract suite that every Store
// passes.
//
// # The Grant is not decoration
//
// Every Repository method takes an auth.Grant and the tenant comes out of it. A
// cache key shared across tenants is a data leak with a fast path, and it is the
// kind that survives review because the query underneath was correct. The key is
// cache:<tenant>:<namespace>:<key>, and a Grant carrying no tenant -- or a
// tenant that could be read as a separator -- is an error, not a global bucket.
//
// Locks and rate limits are the exceptions: a scheduler lock covers the whole
// instance, and rate limiting happens before anybody has authenticated, so
// neither has a Grant to take a tenant from. Locks and RateLimiter therefore
// take a name and a key, not a Grant.
//
// # Every entry expires
//
// Put requires a ttl. Forever is here, and it is written down as a century
// rather than as an absence, because an entry with no expiry is a second copy of
// the truth and the day it diverges nobody knows it exists. Reach for Forever
// rarely.
//
// A ttl that has already passed is not an entry: Put and PutMany forget the
// keys, and Add reports that it wrote nothing. Increment is the exception, and
// its doc comment says why.
//
// # The stores
//
// ArrayStore is in-process and is the default. FileStore is on disk, shared
// between the processes of one machine. DatabaseStore is a table, and is the
// only one whose locks survive the cache being emptied. NullStore keeps nothing.
// MemoizedStore remembers, for one request, what another store already answered.
// FailoverStore tries several in turn.
//
// The RESP store -- Dragonfly, Redis, Valkey and KeyDB, which are one product to
// this collection -- is hesape/redis, a separate module, so that the driver
// ships in the binaries that use it and in no others.
//
// # Shared stores are linked by import
//
// A store every replica sees is a [SharedStore], and its connector is linked
// the way a database connector is: the module is blank-imported, its init()
// calls [Register], and [Open] builds the store from the driver name the
// configuration holds. [Linked] is the check a boot makes first, and a driver
// the binary does not link is an error naming the module and the import line,
// not a nil store inside the first request. The store [Open] returns sits
// under a [Repository] like any other, which is where the tenant enters the
// key.
package cache
