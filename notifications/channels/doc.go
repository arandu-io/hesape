// Package channels is the three ways a notification can be delivered: [Mail]
// sends it as an e-mail, [Database] stores it as a row, and [Broadcast] pushes
// it to a browser that is connected right now.
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
// Mail names a Mailer and Broadcast names a Broadcaster, the little each
// channel needs, so that either can be exercised without the real thing.
// [MailVia] builds the mail channel over the application's *mail.Mailer, and
// that import costs nothing a project does not already link: hesape/mail holds
// the envelope and the rendering, and its transports and views are interfaces
// the application fills in. Broadcast still names its Broadcaster rather than
// importing hesape/broadcasting, whose driver registry would otherwise stand
// behind every package that sends a notification.
package channels
