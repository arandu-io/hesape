// Package events names the three things that can happen to a notification on
// its way out, and builds the hesape/events value that records each of them.
//
// The three are [Sending], [Sent] and [Failed], built by [NewSending],
// [NewSent] and [NewFailed], and what each of them carries is [Payload].
//
// They are event names on the outbox rather than types a listener registers
// for, because the outbox is the one way an application learns that something
// happened: a second listener registry beside it would be a second way to hear
// the same thing.
//
// The constructors take strings rather than the notifications types, so that
// hesape/notifications can import this package to emit them. The payload is
// what a consumer needs and nothing that would drag a row along.
//
// [BroadcastNotificationCreated] is the fourth value here, and it is not one of
// the three: it is what the broadcast channel builds and hands to a
// broadcaster, not a record of something that already happened.
package events
