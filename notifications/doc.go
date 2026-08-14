// Package notifications delivers one short message to one person over the
// channels that person can be reached on.
//
// It mirrors Illuminate\Notifications. The files it answers to, in the clone at
// laravel_illuminate/notifications (Laravel 13, illuminate/notifications
// ^13.0):
//
//	Action.php                          -> messages.Action
//	AnonymousNotifiable.php             -> Anonymous, Route, Routes
//	ChannelManager.php                  -> Notifier (there is no manager to
//	                                       resolve a driver from: the channels
//	                                       are the argument)
//	Channels/                           -> notifications/channels
//	Console/                            -> notifications/console
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
// # The methods of the component with no answer here
//
// Every one of them, with the numbered reason from ADR 0044: (1) a PHP language
// feature Go does not have, (2) a method that only serves the container, a
// facade or a service provider, (3) a driver this ecosystem does not carry.
//
// Notification::fake -- reason 2. It is Facade::swap putting a
// NotificationFake where the ChannelManager was bound, so that every send in
// the application is recorded instead of delivered. There is no container
// (ADR 0001) and no facade (ADR 0002), so there is no binding to swap, and a
// package-level notifier a test could swap would be shared mutable state that
// two tests calling t.Parallel would fight over (ADR 0045).
//
// [Capture] is what a test writes instead, and it is passed in rather than
// installed:
//
//	chans, sent := notifications.Capture(notifications.ChannelMail)
//	n := notifications.New(chans)
//
//	payInvoice(ctx, g, n)
//
//	if !sent.Sent("billing.invoice-paid", customer) {
//		t.Fatal("the customer was not told the invoice was paid")
//	}
//
// The channels are a slice the test owns and [Deliveries] is the recording it
// holds, so two tests running in parallel record into two different ones and
// neither can see the other's. Capture with no arguments takes the three
// channels the collection implements, which is the "was anything sent at all"
// case Notification::fake answers with no arguments too.
//
// NotificationServiceProvider -- reason 2, the whole class. ADR 0001 removed
// the container and ADR 0002 the service provider:
//
//   - NotificationServiceProvider::register binds ChannelManager as the
//     'Illuminate\Notifications\ChannelManager' singleton and aliases the two
//     dispatcher contracts onto it. [New] takes the channels as an argument.
//   - NotificationServiceProvider::boot registers the component's view
//     namespace and the Blade `@notification` directive. The view layer here is
//     kyse, which resolves a view by name at build time and has no namespace to
//     register.
//
// ChannelManager -- reason 2, the driver factory half of Illuminate\Support\
// Manager. A driver resolved from a configuration string at send time is a
// channel a notification can name by accident; here the channels are the slice
// passed to [New]:
//
//   - ChannelManager::createDriver, ::createMailDriver, ::createDatabaseDriver
//     and ::createBroadcastDriver each build one channel out of the container.
//   - ChannelManager::resolveNotificationSender builds the NotificationSender
//     from four container bindings. [Notifier] is both classes in one type.
//
// NotificationSender -- reason 2 for the queueing half, and the rest is
// protected and inlined into [Notifier.SendNow]:
//
//   - NotificationSender::queueNotification pushes SendQueuedNotifications onto
//     the bus when the notification implements ShouldQueue. Nothing queues
//     itself here: [SendQueuedNotifications] is pushed at the call site, so a
//     call that blocks for two seconds looks different from one that does not.
//   - NotificationSender::preferredLocale, ::sendToNotifiable,
//     ::shouldSendNotification and ::formatNotifiables are protected and are
//     the body of [Notifier.SendNow].
//
// SendQueuedNotifications::__clone -- reason 1. PHP's clone hook, which
// deep-copies the wrapped collection. A Go value is copied by assignment.
//
// The traits -- reason 1 and reason 2. SerializesModels, Queueable,
// InteractsWithQueue, ReadsQueueAttributes, Localizable, Macroable and
// ResolvesQueueRoutes are PHP trait composition: horizontal reuse for a
// language with single inheritance, plus a serializer that rebuilds an Eloquent
// model from an id after a queue round trip. Go embeds a struct, and a job here
// carries identities rather than models.
//
// The protected builders -- there is nothing to answer. MailChannel::buildView,
// ::buildMarkdownHtml, ::buildMarkdownText, ::markdownRenderer,
// ::additionalMessageData, ::buildMessage, ::addressMessage, ::addSender,
// ::getRecipients, ::addAttachments and ::runCallbacks; DatabaseChannel::
// buildPayload and ::getData; BroadcastChannel::getData; MailMessage::
// parseAddresses and ::arrayOfAddresses; SimpleMessage::formatLine. Each is the
// body of the exported method above it, and each is unexported here for the
// same reason it is protected there.
//
// Notifiable.php declares no method at all: it is `use HasDatabaseNotifications,
// RoutesNotifications`, and [RoutesNotifications] and
// [HasDatabaseNotifications] are the two halves.
package notifications
