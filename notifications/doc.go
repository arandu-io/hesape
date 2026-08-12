// Package notifications delivers one short message to one person over the
// channels that person can be reached on.
//
// It mirrors Illuminate\Notifications. The files it answers to, in the clone at
// laravel_illuminate/notifications (Laravel 13, illuminate/notifications
// ^13.0):
//
//	Action.php                          -> messages.Action
//	AnonymousNotifiable.php             -> Anonymous, Route
//	ChannelManager.php                  -> Notifier (there is no manager to
//	                                       resolve a driver from: the channels
//	                                       are the argument)
//	Channels/                           -> notifications/channels
//	DatabaseNotification.php            -> Record
//	DatabaseNotificationCollection.php  -> Records
//	Events/                             -> notifications/events
//	HasDatabaseNotifications.php        -> HasDatabaseNotifications, Store
//	Messages/                           -> notifications/messages
//	Notifiable.php                      -> Notifiable, RoutesNotifications and
//	                                       HasDatabaseNotifications, which is
//	                                       the two traits it is made of
//	Notification.php                    -> Notification, NotificationBase
//	NotificationSender.php              -> Notifier.Send, Notifier.SendNow
//	NotificationServiceProvider.php     -> nothing (ADR 0001, ADR 0002)
//	RoutesNotifications.php             -> RoutesNotifications
//	SendQueuedNotifications.php         -> SendQueuedNotifications
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
// A model reaches the same thing under Illuminate's spelling by embedding
// RoutesNotifications and HasDatabaseNotifications, which is what
// `use Notifiable` does in PHP.
//
// # Notification is an interface, and Notification is also a base class
//
// In Illuminate a notification extends the Notification class, which gives it
// an id and a locale. Here Notification is the interface a notification
// satisfies -- two methods, checked by the compiler -- and NotificationBase is
// the state it embeds. A notification that needs neither embeds nothing.
//
// # What is deliberately absent
//
// No ShouldQueue. Laravel decides between "send now" and "send later" by an
// interface the notification class implements somewhere else, which makes a
// call that sometimes blocks for two seconds and sometimes does not, with
// nothing at the call site to say which. Sending on the queue here is
// SendQueuedNotifications, pushed like any other job.
//
// No notification theme, and no second way to draw a message body: RULE 9
// refuses the second way and RULE 13 would drag Node in for the theme. A mail
// notification carries structured lines and an action; messages.Mail.Render
// draws the HTML from them and messages.Mail.PlainText the text, and the two
// cannot disagree. A message that names a template hands it to the view layer,
// which is a name and not an asset pipeline.
//
// No `Notification::` facade and no driver strings resolved from configuration
// at send time. The channels an application has are the slice passed to New.
//
// # The two methods of the component with no answer here
//
// Both are on NotificationServiceProvider, and both are reason 2 of the porting
// rule -- a method that exists only to serve the container and the service
// provider, which ADR 0001 and ADR 0002 removed:
//
//   - register() binds ChannelManager as the 'Illuminate\Notifications\
//     ChannelManager' singleton and aliases the two dispatcher contracts onto
//     it. [New] takes the channels as an argument instead.
//   - boot() registers the notification component's view namespace and the
//     Blade `@notification` directive. The view layer here is kyse, which
//     resolves a view by name at build time and has no namespace to register.
package notifications
