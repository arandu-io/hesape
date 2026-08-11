// Package events mirrors Illuminate\Cache\Events.
//
// Every event a Repository fires lives here, with the fields Laravel puts on
// it, under the names Laravel gives them. A listener written against
// events.CacheHit reads e.Key, e.Value and e.StoreName exactly as the PHP one
// reads $event->key, $event->value and $event->storeName.
//
// They are values, not an interface. Nothing here dispatches: cache.Dispatcher
// is the one method the Repository needs, and what is on the other side of it
// is the application's business.
//
// The files it answers to, in the clone at
// laravel_illuminate/cache/Events:
//
//	CacheEvent.php            -> CacheEvent
//	CacheFailedOver.php       -> CacheFailedOver
//	CacheFlushFailed.php      -> CacheFlushFailed
//	CacheFlushed.php          -> CacheFlushed
//	CacheFlushing.php         -> CacheFlushing
//	CacheHit.php              -> CacheHit
//	CacheLocksFlushFailed.php -> CacheLocksFlushFailed
//	CacheLocksFlushed.php     -> CacheLocksFlushed
//	CacheLocksFlushing.php    -> CacheLocksFlushing
//	CacheMissed.php           -> CacheMissed
//	ForgettingKey.php         -> ForgettingKey
//	KeyForgetFailed.php       -> KeyForgetFailed
//	KeyForgotten.php          -> KeyForgotten
//	KeyWriteFailed.php        -> KeyWriteFailed
//	KeyWritten.php            -> KeyWritten
//	RetrievingKey.php         -> RetrievingKey
//	RetrievingManyKeys.php    -> RetrievingManyKeys
//	WritingKey.php            -> WritingKey
//	WritingManyKeys.php       -> WritingManyKeys
package events
