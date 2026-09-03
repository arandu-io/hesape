package mail_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/mail"
	"github.com/arandu-io/hesape/mail/mailables"
	"github.com/arandu-io/hesape/mail/transport"
)

// welcome is the shape a person writes: an envelope, a content, and nothing
// else. Content.Text is a view name, not a body.
type welcome struct {
	Name string
	Body string
}

func (w welcome) Envelope() mail.Envelope { return mail.Envelope{Subject: "Welcome, " + w.Name} }
func (w welcome) Content() mail.Content   { return mail.Content{Text: w.Body} }

// OrderShipped names no subject, so the one it goes out with is derived from
// its type.
type OrderShipped struct{}

func (OrderShipped) Envelope() mail.Envelope { return mail.Envelope{} }
func (OrderShipped) Content() mail.Content   { return mail.Content{HTMLString: "<p>on its way</p>"} }

// echoView answers with the name of the view it was asked for, so a test can
// put the body it wants straight into the view name.
type echoView struct{}

func (echoView) RenderToString(name string, _ any) (string, error) { return name, nil }

func mailer(t *testing.T) (*mail.Mailer, *transport.Array) {
	t.Helper()
	box := &transport.Array{}
	m := mail.New("array", echoView{}, box, nil)
	m.AlwaysFrom("app@example.test", "App")
	return m, box
}

// TestAMessageIsSentToWhoItWasAddressedTo is the happy path, and it checks the
// default sender too: an application sends from one address, and a mailable that
// does not name one should not have to.
func TestAMessageIsSentToWhoItWasAddressedTo(t *testing.T) {
	m, box := mailer(t)

	if _, err := m.To("you@example.test").Send(context.Background(), welcome{Name: "Ada", Body: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	sent, ok := box.Last()
	if !ok {
		t.Fatal("nothing was sent")
	}
	if got := sent.To[0].Address; got != "you@example.test" {
		t.Errorf("to = %q", got)
	}
	if sent.From.Address != "app@example.test" {
		t.Errorf("from = %q, want the configured default", sent.From.Address)
	}
	if sent.Subject != "Welcome, Ada" {
		t.Errorf("subject = %q", sent.Subject)
	}
	if sent.Text != "hi" {
		t.Errorf("text = %q", sent.Text)
	}
}

// TestTheTransportsReceiptReachesTheCaller.
//
// The provider's identifier is the only thing joining a line in the application
// log to a row in the provider's dashboard, and it used to be dropped by the
// Mailer: Transport.Send returned an error and nothing else.
func TestTheTransportsReceiptReachesTheCaller(t *testing.T) {
	m := mail.New("receipt", echoView{}, receipt{id: "prov-42"}, nil)
	m.AlwaysFrom("app@example.test")

	sent, err := m.To("you@example.test").Send(context.Background(), welcome{Name: "Ada", Body: "hi"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent.ID != "prov-42" {
		t.Errorf("ID = %q, want the provider's identifier", sent.ID)
	}
	if sent.Transport != "receipt" {
		t.Errorf("Transport = %q", sent.Transport)
	}
	if sent.Message == nil || sent.Message.Subject != "Welcome, Ada" {
		t.Error("the receipt does not carry the message it is a receipt for")
	}
}

type receipt struct{ id string }

func (receipt) Name() string { return "receipt" }
func (r receipt) Send(context.Context, mail.Message) (mail.SentMessage, error) {
	return mail.SentMessage{ID: r.id, Transport: r.Name()}, nil
}

// TestNothingIsSentWithoutARecipient.
//
// An error rather than a silent no-op. A message with nobody to send it to is a
// message somebody meant to send, and returning nil here is how a password reset
// goes missing with a green log line.
func TestNothingIsSentWithoutARecipient(t *testing.T) {
	m, box := mailer(t)

	_, err := m.To([]string{}).Send(context.Background(), welcome{Name: "Ada", Body: "hi"})
	if !errors.Is(err, mail.ErrNoRecipient) {
		t.Fatalf("err = %v, want ErrNoRecipient", err)
	}
	if len(box.Messages()) != 0 {
		t.Error("a message with no recipient reached the transport")
	}
}

// TestABadAddressIsRefusedBeforeTheTransport: a typo fails at the call rather
// than as a bounce three minutes later, in a log nobody is reading.
func TestABadAddressIsRefusedBeforeTheTransport(t *testing.T) {
	m, box := mailer(t)

	if _, err := m.To("not an address").Send(context.Background(), welcome{Name: "Ada", Body: "hi"}); err == nil {
		t.Fatal("an unparseable address was accepted")
	}
	if len(box.Messages()) != 0 {
		t.Error("it reached the transport anyway")
	}
}

// TestASubjectIsDerivedFromTheMailableWhenNobodyNamedOne.
//
// The subject falls back to the mailable's type name, humanised, so an
// OrderShipped goes out as "Order Shipped" rather than being refused for having
// no subject.
func TestASubjectIsDerivedFromTheMailableWhenNobodyNamedOne(t *testing.T) {
	m, box := mailer(t)

	if _, err := m.To("you@example.test").Send(context.Background(), OrderShipped{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	sent, _ := box.Last()
	if sent.Subject != "Order Shipped" {
		t.Errorf("subject = %q, want the humanised type name", sent.Subject)
	}
}

// TestAPreRenderedBodyBeatsAView: a literal body is checked before the view and
// before the markdown.
func TestAPreRenderedBodyBeatsAView(t *testing.T) {
	m, box := mailer(t)

	if _, err := m.To("you@example.test").Send(context.Background(), literalAndView{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	sent, _ := box.Last()
	if sent.HTML != "<p>composed elsewhere</p>" {
		t.Errorf("HTML = %q, want the pre-rendered body", sent.HTML)
	}
}

type literalAndView struct{}

func (literalAndView) Envelope() mail.Envelope { return mail.Envelope{Subject: "Both"} }
func (literalAndView) Content() mail.Content {
	return mail.Content{View: "mail.note", HTMLString: "<p>composed elsewhere</p>"}
}

// TestARepeatedAddressKeepsItsLastSpelling.
//
// The dedupe keeps the last spelling of a repeated address. Getting it
// backwards is invisible until somebody asks why the display name went missing.
func TestARepeatedAddressKeepsItsLastSpelling(t *testing.T) {
	m, box := mailer(t)

	_, err := m.To("you@example.test").
		To("you@example.test", "Ada").
		Send(context.Background(), welcome{Name: "Ada", Body: "hi"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	sent, _ := box.Last()
	if len(sent.To) != 1 {
		t.Fatalf("the address was sent twice: %v", sent.To)
	}
	if sent.To[0].Name != "Ada" {
		t.Errorf("name = %q, want the last spelling", sent.To[0].Name)
	}
}

// TestAGlobalToRedirectsEverythingAndSaysWhoItWasFor.
//
// alwaysTo is what a staging environment sets so a test run cannot reach a
// customer. Without the X-To header the message that arrives no longer says
// who it was addressed to, which makes the redirect useless for the one thing
// it is for.
func TestAGlobalToRedirectsEverythingAndSaysWhoItWasFor(t *testing.T) {
	m, box := mailer(t)
	m.AlwaysTo("inbox@example.test")

	_, err := m.To("customer@example.test").CC("boss@example.test").
		Send(context.Background(), welcome{Name: "Ada", Body: "hi"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	sent, _ := box.Last()
	if len(sent.To) != 1 || sent.To[0].Address != "inbox@example.test" {
		t.Fatalf("to = %v, want the global address alone", sent.To)
	}
	if len(sent.CC) != 0 {
		t.Errorf("the carbon copy still went out: %v", sent.CC)
	}
	if got := sent.Headers.Text["X-To"]; !strings.Contains(got, "customer@example.test") {
		t.Errorf("X-To = %q, want the address the message was really for", got)
	}
	if got := sent.Headers.Text["X-Cc"]; !strings.Contains(got, "boss@example.test") {
		t.Errorf("X-Cc = %q", got)
	}
}

// TestAListenerCanStopTheMessage. MessageSending is the only event a listener
// can refuse, and a suppression list is what that is for.
func TestAListenerCanStopTheMessage(t *testing.T) {
	box := &transport.Array{}
	events := &recorder{allow: false}
	m := mail.New("array", echoView{}, box, events)
	m.AlwaysFrom("app@example.test")

	if _, err := m.To("you@example.test").Send(context.Background(), welcome{Name: "Ada", Body: "hi"}); err != nil {
		t.Fatalf("a refused message is not an error: %v", err)
	}
	if len(box.Messages()) != 0 {
		t.Error("the listener refused and the message went out anyway")
	}
	if len(events.sending) != 1 {
		t.Errorf("MessageSending fired %d times", len(events.sending))
	}
	if len(events.sent) != 0 {
		t.Error("MessageSent fired for a message that was never sent")
	}
}

// TestMessageSentCarriesTheReceipt, which is the only event a log line can
// join to a row in a provider's dashboard.
func TestMessageSentCarriesTheReceipt(t *testing.T) {
	events := &recorder{allow: true}
	m := mail.New("receipt", echoView{}, receipt{id: "prov-7"}, events)
	m.AlwaysFrom("app@example.test")

	if _, err := m.To("you@example.test").Send(context.Background(), welcome{Name: "Ada", Body: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(events.sent) != 1 {
		t.Fatalf("MessageSent fired %d times", len(events.sent))
	}
	if events.sent[0].Sent.ID != "prov-7" {
		t.Errorf("the event does not carry the provider's identifier: %+v", events.sent[0].Sent)
	}
	if events.sent[0].GetOriginalMessage() == nil {
		t.Error("the event does not carry the message that was sent")
	}
}

type recorder struct {
	allow   bool
	sending []mail.MessageSending
	sent    []mail.MessageSent
}

func (r *recorder) Until(_ context.Context, event any) bool {
	if e, ok := event.(mail.MessageSending); ok {
		r.sending = append(r.sending, e)
	}
	return r.allow
}

func (r *recorder) Dispatch(_ context.Context, event any) {
	if e, ok := event.(mail.MessageSent); ok {
		r.sent = append(r.sent, e)
	}
}

// TestTheAssertionsReadTheMessageAsItWillGoOut, which is what Build produces.
func TestTheAssertionsReadTheMessageAsItWillGoOut(t *testing.T) {
	m, _ := mailer(t)

	msg, err := m.To("you@example.test", "Ada").
		CC("boss@example.test").
		BCC("audit@example.test").
		ReplyTo("support@example.test").
		Tag("welcome").
		Metadata("plan", "pro").
		Build(context.Background(), welcome{Name: "Ada", Body: "Open the link."})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	msg.AssertHasTo(t, "you@example.test", "Ada").
		AssertTo(t, "you@example.test").
		AssertHasCC(t, "boss@example.test").
		AssertHasBCC(t, "audit@example.test").
		AssertHasReplyTo(t, "support@example.test").
		AssertFrom(t, "app@example.test").
		AssertHasSubject(t, "Welcome, Ada").
		AssertHasTag(t, "welcome").
		AssertHasMetadata(t, "plan", "pro").
		AssertSeeInText(t, "Open the link.").
		AssertDontSeeInText(t, "unsubscribe")
}

// TestAssertSeeInHtmlEscapesWhatItLooksFor by default, because the body it is
// searching is escaped: asserting on "Ada & Co" against "Ada &amp; Co" fails
// for a reason that has nothing to do with the message.
func TestAssertSeeInHtmlEscapesWhatItLooksFor(t *testing.T) {
	m, _ := mailer(t)

	msg, err := m.To("you@example.test").Build(context.Background(), escapedBody{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	msg.AssertSeeInHTML(t, "Ada & Co").
		AssertSeeInHTML(t, "<strong>", false).
		AssertDontSeeInHTML(t, "Ada & Sons").
		AssertSeeInOrderInHTML(t, []string{"Ada & Co", "invoice"})
}

type escapedBody struct{}

func (escapedBody) Envelope() mail.Envelope { return mail.Envelope{Subject: "Invoice"} }
func (escapedBody) Content() mail.Content {
	return mail.Content{HTMLString: "<p><strong>Ada &amp; Co</strong> invoice</p>"}
}

// TestAnAttachmentBuiltFromDataReachesTheMessage, and can be asserted on.
func TestAnAttachmentBuiltFromDataReachesTheMessage(t *testing.T) {
	m, _ := mailer(t)

	invoice := mailables.FromData(func() ([]byte, error) { return []byte("%PDF-1.7"), nil }, "invoice.pdf").
		WithMime("application/pdf")

	msg, err := m.To("you@example.test").Attach(invoice).
		Build(context.Background(), welcome{Name: "Ada", Body: "hi"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !msg.HasAttachedData([]byte("%PDF-1.7"), "invoice.pdf", mail.AttachOptions{Mime: "application/pdf"}) {
		t.Fatalf("the attachment is missing: %+v", msg.RawAttachments)
	}
	msg.AssertHasAttachedData(t, []byte("%PDF-1.7"), "invoice.pdf", mail.AttachOptions{Mime: "application/pdf"})
}

// TestAnAttachmentFromDataWithNoNameIsRefused.
//
// A part with no filename arrives as "noname" in every client, which is what
// the refusal exists to prevent.
func TestAnAttachmentFromDataWithNoNameIsRefused(t *testing.T) {
	unnamed := mail.FromData(func() ([]byte, error) { return []byte("x"), nil })

	err := unnamed.AttachTo(&mail.Message{})
	if !errors.Is(err, mail.ErrAttachmentNeedsName) {
		t.Fatalf("err = %v, want ErrAttachmentNeedsName", err)
	}

	// A fluent setter has nowhere to return an error, so it keeps one and the
	// send answers it. Dropping it there is how an invoice goes out without the
	// invoice, reported as a success.
	m, box := mailer(t)
	_, err = m.To("you@example.test").Attach(unnamed).
		Send(context.Background(), welcome{Name: "Ada", Body: "hi"})
	if !errors.Is(err, mail.ErrAttachmentNeedsName) {
		t.Errorf("Send err = %v, want the attachment failure carried out of the fluent call", err)
	}
	if len(box.Messages()) != 0 {
		t.Error("a message whose attachment could not be built was sent without it")
	}
}

// TestTwoDescriptionsOfTheSameFileAreTheSameAttachment. It is what makes
// assertHasAttachment work against an attachment the mailable declared rather
// than one the test added.
func TestTwoDescriptionsOfTheSameFileAreTheSameAttachment(t *testing.T) {
	one := mail.FromPath("/tmp/invoice.pdf").As("invoice.pdf").WithMime("application/pdf")
	same := mail.FromPath("/tmp/invoice.pdf").As("invoice.pdf").WithMime("application/pdf")
	other := mail.FromPath("/tmp/receipt.pdf").As("invoice.pdf").WithMime("application/pdf")

	if !one.IsEquivalent(same) {
		t.Error("two descriptions of the same file did not match")
	}
	if one.IsEquivalent(other) {
		t.Error("two different files matched")
	}
}

// TestAMailableDeclaringItsOwnAttachmentsAndHeadersIsRead.
//
// Both methods are optional: a mailable that declares them is asked, and one
// that does not is not.
func TestAMailableDeclaringItsOwnAttachmentsAndHeadersIsRead(t *testing.T) {
	m, _ := mailer(t)

	msg, err := m.To("you@example.test").Build(context.Background(), threaded{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if msg.Headers.MessageID != "reply-2@example.test" {
		t.Errorf("Message-Id = %q", msg.Headers.MessageID)
	}
	if got := msg.Headers.ReferencesString(); got != "<first@example.test>" {
		t.Errorf("References = %q, want the ids in angle brackets", got)
	}
	if !msg.HasAttachedData([]byte("note"), "note.txt") {
		t.Errorf("the mailable's own attachment is missing: %+v", msg.RawAttachments)
	}
}

type threaded struct{}

func (threaded) Envelope() mail.Envelope { return mail.Envelope{Subject: "Re: hello"} }
func (threaded) Content() mail.Content   { return mail.Content{HTMLString: "<p>hi</p>"} }
func (threaded) Headers() mail.Headers {
	return mail.Headers{MessageID: "reply-2@example.test", References: []string{"first@example.test"}}
}
func (threaded) Attachments() []*mail.Attachment {
	return []*mail.Attachment{
		mail.FromData(func() ([]byte, error) { return []byte("note"), nil }, "note.txt"),
	}
}

// TestQueueingWithNoQueueWiredIsAnError, rather than a message that reports
// success and is never sent.
func TestQueueingWithNoQueueWiredIsAnError(t *testing.T) {
	m, _ := mailer(t)

	if _, err := m.Queue(context.Background(), welcome{Name: "Ada"}); !errors.Is(err, mail.ErrNoQueue) {
		t.Errorf("err = %v, want ErrNoQueue", err)
	}
	if _, err := m.Later(context.Background(), time.Minute, welcome{Name: "Ada"}); !errors.Is(err, mail.ErrNoQueue) {
		t.Errorf("err = %v, want ErrNoQueue", err)
	}
}

// TestAQueuedMailableCarriesTheAddressingTheCallSiteGave.
//
// The mailable is somebody else's struct and carries no addresses of its own,
// so the job has to carry the pending message or a queued send goes to nobody.
func TestAQueuedMailableCarriesTheAddressingTheCallSiteGave(t *testing.T) {
	box := &transport.Array{}
	m := mail.New("array", echoView{}, box, nil)
	m.AlwaysFrom("app@example.test")

	q := &fakeQueue{}
	m.SetQueue(q)

	if _, err := m.To("you@example.test").Queue(context.Background(), welcome{Name: "Ada", Body: "hi"}); err != nil {
		t.Fatalf("Queue: %v", err)
	}

	job, ok := q.pushed.(*mail.SendQueuedMailable)
	if !ok {
		t.Fatalf("pushed %T, want a *mail.SendQueuedMailable", q.pushed)
	}
	if !strings.HasSuffix(job.DisplayName(), "welcome") {
		t.Errorf("DisplayName = %q, want the mailable's type", job.DisplayName())
	}

	if err := job.Handle(context.Background(), factory{mailer: m}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	sent, ok := box.Last()
	if !ok {
		t.Fatal("the worker sent nothing")
	}
	if len(sent.To) != 1 || sent.To[0].Address != "you@example.test" {
		t.Errorf("to = %v, want the address the call site gave", sent.To)
	}
}

type fakeQueue struct{ pushed any }

func (q *fakeQueue) Connection(string) mail.Queue { return q }
func (q *fakeQueue) PushOn(_ context.Context, _ string, job any) (string, error) {
	q.pushed = job
	return "job-1", nil
}
func (q *fakeQueue) LaterOn(_ context.Context, _ string, _ time.Duration, job any) (string, error) {
	q.pushed = job
	return "job-1", nil
}

type factory struct{ mailer *mail.Mailer }

func (f factory) Mailer(string) (*mail.Mailer, error) { return f.mailer, nil }

// TestAnUnknownTransportIsNamedInTheError, because "unsupported mail transport"
// without the name sends somebody to the configuration file to guess.
func TestAnUnknownTransportIsNamedInTheError(t *testing.T) {
	manager := mail.NewMailManager(mail.ManagerConfig{
		Default: "smtp",
		Mailers: map[string]mail.MailerConfig{"smtp": {Transport: "carrier-pigeon"}},
	}, echoView{}, nil)

	_, err := manager.Mailer("")
	if err == nil || !strings.Contains(err.Error(), "carrier-pigeon") {
		t.Fatalf("err = %v, want the transport named", err)
	}

	if _, err := manager.Mailer("nowhere"); err == nil || !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("err = %v, want the mailer named", err)
	}
}

// TestTheManagerBuildsOnceAndPurgeUndoesIt.
func TestTheManagerBuildsOnceAndPurgeUndoesIt(t *testing.T) {
	manager := mail.NewMailManager(mail.ManagerConfig{
		Default: "array",
		Mailers: map[string]mail.MailerConfig{"array": {Transport: "array"}},
		From:    mail.Address{Address: "app@example.test", Name: "App"},
	}, echoView{}, nil)
	transport.Register(manager)

	first, err := manager.Mailer("")
	if err != nil {
		t.Fatalf("Mailer: %v", err)
	}
	again, _ := manager.Driver("array")
	if first != again {
		t.Error("the manager built the mailer twice")
	}

	manager.Purge("array")
	third, _ := manager.Mailer("array")
	if third == first {
		t.Error("Purge did not forget the mailer")
	}

	if manager.GetDefaultDriver() != "array" {
		t.Errorf("GetDefaultDriver = %q", manager.GetDefaultDriver())
	}
	manager.SetDefaultDriver("other")
	if manager.GetDefaultDriver() != "other" {
		t.Error("SetDefaultDriver did not take")
	}
	manager.ForgetMailers()
}

// TestAFailoverTransportIsBuiltFromTheMailersItNames, and building it must not
// deadlock: the creator calls back into the manager that is calling it.
func TestAFailoverTransportIsBuiltFromTheMailersItNames(t *testing.T) {
	manager := mail.NewMailManager(mail.ManagerConfig{
		Default: "backup",
		Mailers: map[string]mail.MailerConfig{
			"backup": {Transport: "failover", Mailers: []string{"log", "array"}},
			"log":    {Transport: "log"},
			"array":  {Transport: "array"},
		},
	}, echoView{}, nil)
	transport.Register(manager)

	done := make(chan error, 1)
	go func() {
		_, err := manager.Mailer("")
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Mailer: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("building a failover transport deadlocked the manager")
	}
}

// TestTheMarkdownRendererDrawsBothPartsFromOneTemplate, which is the whole
// reason markdown mailables exist.
func TestTheMarkdownRendererDrawsBothPartsFromOneTemplate(t *testing.T) {
	box := &transport.Array{}
	m := mail.New("array", markdownView{}, box, nil)
	m.AlwaysFrom("app@example.test")
	m.SetMarkdown(mail.NewMarkdown(markdownView{}, mail.MarkdownConfig{}))

	if _, err := m.To("you@example.test").Send(context.Background(), announcement{}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	sent, _ := box.Last()
	if !strings.Contains(sent.HTML, "the body") {
		t.Errorf("the HTML part is missing the template: %q", sent.HTML)
	}
	if !strings.Contains(sent.HTML, "<style") {
		t.Errorf("the theme's stylesheet did not reach the message: %q", sent.HTML)
	}
	if sent.Text == "" {
		t.Error("no plain-text part was drawn, so every client that cannot render HTML shows nothing")
	}
	if strings.Contains(sent.Text, "\n\n\n") {
		t.Errorf("the blank lines were not collapsed: %q", sent.Text)
	}
}

type announcement struct{}

func (announcement) Envelope() mail.Envelope { return mail.Envelope{Subject: "News"} }
func (announcement) Content() mail.Content   { return mail.Content{Markdown: "mail.news"} }

type markdownView struct{}

func (markdownView) RenderToString(name string, _ any) (string, error) {
	if strings.HasPrefix(name, "mail.themes.") {
		return "body { color: #222 }", nil
	}
	return "# Heading\n\n\n\nthe body\n", nil
}

// TestParseEscapesTheSourceWhenSecuredEncodingIsOn.
//
// The markdown being parsed came from a value somebody typed, and a
// [label](javascript:...) in it is a link the reader did not write.
func TestParseEscapesTheSourceWhenSecuredEncodingIsOn(t *testing.T) {
	t.Cleanup(mail.FlushState)

	loose := mail.Parse("[click](https://example.test)")
	if !strings.Contains(loose, "<a") {
		t.Errorf("a plain link was not rendered: %q", loose)
	}

	mail.WithSecuredEncoding()
	if !mail.SecuredEncoding() {
		t.Fatal("WithSecuredEncoding did not take")
	}
	tight := mail.Parse("[click](https://example.test)")
	if strings.Contains(tight, ">click</a>") {
		t.Errorf("the bracket was still a labelled link with secured encoding on: %q", tight)
	}
	if !strings.Contains(tight, "[click]") {
		t.Errorf("the bracket did not survive as text: %q", tight)
	}

	mail.WithoutSecuredEncoding()
	if mail.SecuredEncoding() {
		t.Error("WithoutSecuredEncoding did not take")
	}
}

// TestAnUnsafeLinkIsStripped. A javascript: href in a mail body runs when a
// webmail client renders it.
func TestAnUnsafeLinkIsStripped(t *testing.T) {
	html := mail.Converter().Convert(`<a href="javascript:alert(1)">x</a>`)
	if strings.Contains(html, "javascript:") {
		t.Errorf("the unsafe link survived: %q", html)
	}
}

// TestTheTextPassEmbedsNothing. A cid: reference in the plain-text part arrives
// as literal text in the middle of a sentence.
func TestTheTextPassEmbedsNothing(t *testing.T) {
	msg := &mail.Message{}
	if got := msg.Embed("/tmp/logo.png"); !strings.HasPrefix(got, "cid:") {
		t.Errorf("the HTML pass did not embed: %q", got)
	}

	text := mail.NewTextMessage(msg)
	if got := text.Embed("/tmp/logo.png"); got != "" {
		t.Errorf("Embed = %q, want the empty string", got)
	}
	if got := text.EmbedData([]byte("x"), "logo.png"); got != "" {
		t.Errorf("EmbedData = %q, want the empty string", got)
	}
}

// TestMailablesIsTheSameTypesUnderASecondName. An alias package that declared
// its own types would be a second Envelope, and a mailable written against one
// would not satisfy the other.
func TestMailablesIsTheSameTypesUnderASecondName(t *testing.T) {
	var env mail.Envelope = mailables.Envelope{Subject: "Hello"}
	var content mail.Content = mailables.Content{HTMLString: "<p>hi</p>"}
	var address mail.Address = mailables.NewAddress("you@example.test", "Ada")

	if env.Subject != "Hello" || content.HTMLString == "" || address.Name != "Ada" {
		t.Error("the aliases do not carry the same values")
	}
}

// TestBccIsNotAHeader is the one that leaks if it is wrong.
//
// Blind copy is on the envelope, in RCPT TO. Writing it into the message is how
// every recipient learns who else was copied, and it is one line to get wrong.
func TestBccIsNotAHeader(t *testing.T) {
	rendered := mustRender(t, mail.Message{
		Envelope: mail.Envelope{
			From:    mail.Address{Address: "app@example.test"},
			To:      []mail.Address{{Address: "you@example.test"}},
			BCC:     []mail.Address{{Address: "secret@example.test"}},
			Subject: "Hello",
		},
		Text: "hi",
	})

	if strings.Contains(rendered, "secret@example.test") {
		t.Errorf("the blind copy is in the message:\n%s", rendered)
	}
	if !strings.Contains(rendered, "you@example.test") {
		t.Error("the visible recipient is missing")
	}
}

// TestANonASCIISubjectIsEncoded: "Você tem uma fatura" is the first subject
// anybody writes here, and a raw one arrives as mojibake.
func TestANonASCIISubjectIsEncoded(t *testing.T) {
	rendered := mustRender(t, mail.Message{
		Envelope: mail.Envelope{
			From:    mail.Address{Address: "app@example.test"},
			To:      []mail.Address{{Address: "you@example.test"}},
			Subject: "Você tem uma fatura",
		},
		Text: "hi",
	})

	if strings.Contains(rendered, "Você tem uma fatura") {
		t.Errorf("the subject went out raw:\n%s", rendered)
	}
	if !strings.Contains(rendered, "=?utf-8?q?") {
		t.Errorf("the subject was not encoded:\n%s", rendered)
	}
}

// TestBothPartsAreSentWhenBothExist, in the order the standard asks for: least
// preferred first, so a client picks the last one it understands.
func TestBothPartsAreSentWhenBothExist(t *testing.T) {
	rendered := mustRender(t, mail.Message{
		Envelope: mail.Envelope{
			From:    mail.Address{Address: "app@example.test"},
			To:      []mail.Address{{Address: "you@example.test"}},
			Subject: "Hello",
		},
		HTML: "<p>hello</p>",
		Text: "hello",
	})

	if !strings.Contains(rendered, "multipart/alternative") {
		t.Fatalf("not multipart:\n%s", rendered)
	}
	plain, html := strings.Index(rendered, "text/plain"), strings.Index(rendered, "text/html")
	if plain < 0 || html < 0 {
		t.Fatalf("a part is missing:\n%s", rendered)
	}
	if plain > html {
		t.Error("the HTML part comes first: a client that renders both picks the last it understands, " +
			"so this arrangement shows plain text to everybody")
	}
}

// TestAnAttachmentIsCarriedByTheRenderedMessage, or attach() is a method with
// the right name and no effect.
func TestAnAttachmentIsCarriedByTheRenderedMessage(t *testing.T) {
	msg := mail.Message{
		Envelope: mail.Envelope{
			From:    mail.Address{Address: "app@example.test"},
			To:      []mail.Address{{Address: "you@example.test"}},
			Subject: "Invoice",
		},
		Text: "attached",
	}
	msg.AttachData([]byte("%PDF-1.7"), "invoice.pdf", mail.AttachOptions{Mime: "application/pdf"})

	rendered := mustRender(t, msg)
	if !strings.Contains(rendered, "multipart/mixed") {
		t.Fatalf("the message is not multipart/mixed:\n%s", rendered)
	}
	if !strings.Contains(rendered, `filename="invoice.pdf"`) {
		t.Errorf("the attachment has no filename:\n%s", rendered)
	}
	if !strings.Contains(rendered, "application/pdf") {
		t.Errorf("the attachment lost its content type:\n%s", rendered)
	}
}

// TestALongLineIsFolded. A body line over 998 bytes is refused by the protocol,
// and an HTML document is one long line often enough.
func TestALongLineIsFolded(t *testing.T) {
	rendered := mustRender(t, mail.Message{
		Envelope: mail.Envelope{
			From:    mail.Address{Address: "app@example.test"},
			To:      []mail.Address{{Address: "you@example.test"}},
			Subject: "Hello",
		},
		HTML: "<p>" + strings.Repeat("x", 2000) + "</p>",
	})

	for _, line := range strings.Split(rendered, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("a line is %d bytes: the server refuses the message", len(line))
		}
	}
}

// TestRetryableSurvivesWrapping. The mark is what a job reads to decide between
// rescheduling and giving up, and it has to survive being wrapped on the way
// out of a transport.
func TestRetryableSurvivesWrapping(t *testing.T) {
	cause := errors.New("the provider answered 503")
	wrapped := errors.Join(mail.Retryable(cause), errors.New("and so did the other one"))

	var mark mail.ErrRetryable
	if !errors.As(wrapped, &mark) {
		t.Fatal("the mark was lost")
	}
	if !errors.Is(wrapped, cause) {
		t.Error("the cause was lost")
	}
	if mail.Retryable(nil) != nil {
		t.Error("Retryable(nil) is an error, so a caller has to guard the call it exists to remove")
	}
}
