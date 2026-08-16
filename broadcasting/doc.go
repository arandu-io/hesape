// Package broadcasting mirrors Illuminate\Broadcasting.
//
// The files it answers to, in the clone at
// laravel_illuminate/broadcasting:
//
//	AnonymousEvent.php
//	BroadcastController.php
//	BroadcastEvent.php
//	BroadcastException.php
//	BroadcastManager.php
//	BroadcastServiceProvider.php
//	Channel.php
//	EncryptedPrivateChannel.php
//	FakePendingBroadcast.php
//	InteractsWithBroadcasting.php
//	InteractsWithSockets.php
//	PendingBroadcast.php
//	PresenceChannel.php
//	PrivateChannel.php
//	UniqueBroadcastEvent.php
//
// The drivers are broadcasting/broadcasters, as they are in the PHP.
//
// # What is not ported, and why
//
// Twelve public methods of the component have no name here. Each one, with the
// ADR 0056 reason number:
//
//	BroadcastManager::pusher, ::ably, PusherBroadcaster::getPusher, ::setPusher,
//	    AblyBroadcaster::getAbly, ::setAbly and ::generateAblySignature --
//	    reason 3, all seven. Pusher and Ably are two hosted services reached
//	    through pusher/pusher-php-server and ably/ably-php, neither of which
//	    this ecosystem carries. What is here is the same contract over what it
//	    does carry: [broadcasters.RedisBroadcaster] publishes on the connection
//	    hesape/redis already opens, [broadcasters.LogBroadcaster] writes the
//	    payload, and [broadcasters.NullBroadcaster] drops it. A driver for a
//	    hosted service is a module of its own, the way filesystem/s3 is
//	    (ADR 0048), and adding one does not change a line of this package --
//	    which is what [BroadcastManager.Extend] is for (RULE 11).
//	BroadcastServiceProvider::provides, BroadcastManager::getApplication and
//	    ::setApplication -- reason 2: they name the bindings the provider made
//	    and hand the container back so a driver closure can resolve out of it.
//	    [NewBroadcastManager] takes what it needs as arguments.
//	UniqueBroadcastEvent::uniqueVia -- reason 2: its body resolves a cache
//	    repository out of the container to hold the uniqueness lock, unless the
//	    event names one itself. The lock here is the [UniqueLock] passed to
//	    [NewBroadcastManager], so there is nothing left to resolve and no second
//	    place an event can name one from (RULE 9).
package broadcasting
