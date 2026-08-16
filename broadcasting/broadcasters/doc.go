// Package broadcasters holds the drivers a broadcast is published through.
//
// [RedisBroadcaster] publishes on Redis pub/sub, [LogBroadcaster] writes the
// payload to a log, and [NullBroadcaster] drops it. All three embed
// [Broadcaster], the channel registry and the authorization walk they share,
// and [Register] puts all three on a manager in one call.
package broadcasters
