// Package notifications delivers one short message to one person over the
// channels that person can be reached on.
//
// It mirrors Illuminate\Notifications. The files it answers to, in the clone at
// laravel_illuminate/notifications:
//
//	Action.php                          -> messages.Action
//	AnonymousNotifiable.php             -> Anonymous, Route
//	ChannelManager.php                  -> Notifier (there is no manager to
//	                                       resolve a driver from: the channels
//	                                       are the argument)
//	DatabaseNotification.php            -> Record
//	DatabaseNotificationCollection.php  -> []Record
//	HasDatabaseNotifications.php        -> Store
//	Notifiable.php                      -> Notifiable
//	Notification.php                    -> Notification
//	NotificationSender.php              -> Notifier.Send, Notifier.SendMany
//	NotificationServiceProvider.php     -> nothing (ADR 0001, ADR 0002)
//	RoutesNotifications.php             -> Notifiable.RouteFor
//	SendQueuedNotifications.php         -> nothing here; sending on the queue is
//	                                       a job that calls Send, and it is
//	                                       visible at the call site
//
// # The shape
//
// A Notification says what it is (a Key) and where it goes (Via). A Notifiable
// says how to reach somebody on a channel (RouteFor). A Channel does the
// delivering. The Notifier puts the three together:
//
//	n := notifications.New([]notifications.Channel{
//		channels.NewMail(mailer),
//		channels.NewDatabase(store),
//	})
//
//	g, err := auth.Authorize(ctx, policy, subject, notifications.ActionSend, user)
//	if err != nil {
//		return err
//	}
//	err = n.Send(ctx, g, user, InvoicePaid{Number: "2026-114"})
//
// The Grant is not decoration. A notification names a person and usually
// carries a fact about their account, and the stored copy is a row like any
// other row -- so it is reachable only through a Policy, on the way in and on
// the way out (RULE 17).
//
// # What is deliberately absent
//
// No ShouldQueue. Laravel decides between "send now" and "send later" by an
// interface the notification class implements somewhere else, which makes a
// call that sometimes blocks for two seconds and sometimes does not, with
// nothing at the call site to say which. Sending on the queue here is a job
// that calls Send.
//
// No markdown notifications and no notification theme. Both are the second way
// to draw a message body, which RULE 9 refuses and RULE 13 would drag Node
// into. A mail notification carries structured lines and an action; the HTML
// is drawn by the view layer from the same lines, and messages.Mail.PlainText
// renders the text part with no template at all.
//
// No ChannelManager, no `Notification::` facade, no driver strings resolved
// from configuration at send time. The channels an application has are the
// slice passed to New.
package notifications
