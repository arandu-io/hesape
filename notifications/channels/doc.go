// Package channels is the three ways the collection can deliver a
// notification.
//
// It mirrors Illuminate\Notifications\Channels. The files it answers to, in the
// clone at laravel_illuminate/notifications/Channels:
//
//	MailChannel.php       -> Mail
//	DatabaseChannel.php   -> Database
//	BroadcastChannel.php  -> Broadcast
//
// A channel is deliberately thin. Authorization, the choice of channels,
// suppression and the events all happened in notifications.Notifier before it
// was called, so a channel does one thing: turn the notification into the shape
// its transport wants, and hand it over.
//
// # What a notification has to implement
//
// Nothing, until it wants a channel. Each channel declares the one method it
// needs -- MailNotification, DatabaseNotification, BroadcastNotification --
// and a notification satisfies the ones it uses. That is why adding a channel
// to a project does not touch the notifications that do not travel on it, and
// why a notification cannot claim a channel it has no body for: naming it in
// Via without the method is an error at the send, with the type name in it.
//
// # The seams
//
// Mail names a Mailer and Broadcast names a Broadcaster rather than importing
// hesape/mail and hesape/broadcasting, which are layers 4 and 5 of
// docs/31-reorganizacao-hesape.md and are not filled yet. Naming the little a
// channel needs is the Go answer either way: a package that imports the whole
// mailer to send one message is a package that cannot be tested without one.
package channels
