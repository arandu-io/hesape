// Package notifications delivers one short message to one person over the
// channels that person can be reached on.
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
// the way out.
//
// A model gets both halves by embedding [RoutesNotifications] and
// [HasDatabaseNotifications].
//
// # The interface and the state
//
// [Notification] is the interface a notification satisfies -- two methods,
// checked by the compiler -- and [NotificationBase] is the state it embeds, an
// id and a locale. A notification that needs neither embeds nothing.
//
// # Sending later, and drawing a body
//
// Nothing queues itself: sending on a queue is [SendQueuedNotifications],
// pushed at the call site like any other job, so a call that blocks for two
// seconds looks different from one that does not.
//
// A mail notification carries structured lines and an action rather than a
// body. messages.Mail.Render draws the HTML from them and
// messages.Mail.PlainText the text, and the two cannot disagree. A message that
// names a template hands it to the view layer, which is a name and not an asset
// pipeline.
//
// The channels an application has are the slice passed to [New], never a driver
// name resolved from configuration at send time: a channel resolved by string
// is a channel a notification can name by accident.
//
// # Testing
//
// [Capture] is what a test sends through, and it is passed in rather than
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
// channels this package implements, which answers "was anything sent at all".
package notifications
