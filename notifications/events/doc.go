// Package events names the three things that can happen to a notification on
// its way out, and builds the hesape/events value that records each of them.
//
// It mirrors Illuminate\Notifications\Events. The files it answers to, in the
// clone at laravel_illuminate/notifications/Events (Laravel 13,
// illuminate/notifications ^13.0):
//
//	NotificationSending.php            -> Sending, NewSending
//	NotificationSent.php               -> Sent, NewSent
//	NotificationFailed.php             -> Failed, NewFailed
//	BroadcastNotificationCreated.php   -> BroadcastNotificationCreated
//
// In Laravel these are classes a listener type-hints. Here they are event names
// on the outbox, because that is the one way an application learns that
// something happened (hesape/events) -- a second listener registry next to it is
// the second way RULE 9 refuses.
//
// The constructors take strings rather than the notifications types so that
// hesape/notifications can import this package to emit them. The payload is what
// a consumer needs and nothing that would drag a row along.
//
// # The methods with no answer here
//
// NotificationSending, NotificationSent and NotificationFailed carry a
// constructor and four public properties each, and nothing else. The four
// properties are [Payload]; the constructors are [NewSending], [NewSent] and
// [NewFailed].
//
// BroadcastNotificationCreated has one more, and it is reason 2 of the porting
// rule -- a method that exists only to serve the framework's own plumbing:
//
//   - channelName() is protected and is [BroadcastNotificationCreated.ChannelName]
//     anyway, because a channel derived from a notifiable is the one thing a
//     caller has to be able to check.
//
// The Queueable and SerializesModels traits the four classes use are reason 1:
// PHP traits that exist so that an object graph survives a JSON round trip
// through the queue, which Go does with a struct and encoding/json.
package events
