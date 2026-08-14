package mail

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// PendingMail is Illuminate\Mail\PendingMail together with the fluent surface
// Illuminate puts on the Mailable class.
//
// # Why the two are one type here
//
// In Laravel a mailable is a class you extend, so the base class can carry
// to(), cc(), subject(), attach(), tag() and the rest, and PendingMail carries
// a smaller copy of the same thing. In Go a mailable is an interface your type
// satisfies -- see [Mailable] -- and there is nowhere to put methods on somebody
// else's struct. The value that is doing the sending carries them instead, and
// it is the value a Laravel developer already has in hand:
//
//	mailer.To("you@example.test").
//		CC("boss@example.test").
//		Tag("welcome").
//		Send(ctx, WelcomeEmail{Name: "Ada"})
//
// Every method name below is Illuminate's, from one class or the other.
type PendingMail struct {
	mailer *Mailer

	locale string

	to      []Address
	cc      []Address
	bcc     []Address
	from    []Address
	replyTo []Address

	subject string

	view         string
	htmlView     string
	textView     string
	markdownView string
	htmlString   string
	viewData     map[string]any

	attachments     []AttachedFile
	rawAttachments  []AttachedData
	diskAttachments []AttachedDisk

	tags     []string
	metadata map[string]string

	callbacks []func(*Message)

	theme      string
	mailerName string
	priority   int

	// err is what a fluent setter could not report; see [Message.Err]. Build
	// answers it, so an attachment that could not be added fails the send
	// rather than going missing from it.
	err error
}

// Err is what went wrong in a fluent call that had no way to say so.
//
// Err has no PHP counterpart: PendingMail throws, and a method that returns
// the receiver cannot.
func (p *PendingMail) Err() error { return p.err }

// viewDataCallback is Illuminate's static Mailable::$viewDataCallback.
var viewDataCallback func(mailable Mailable) map[string]any

// BuildViewDataUsing registers a callback that contributes to every mailable's
// view data, and is Illuminate's static Mailable::buildViewDataUsing().
//
// It is global state, as it is there. Pass nil to clear it.
func BuildViewDataUsing(callback func(mailable Mailable) map[string]any) {
	viewDataCallback = callback
}

// Locale sets the locale the message is rendered in.
//
// Locale is PendingMail::locale, and Mailable::locale.
func (p *PendingMail) Locale(locale string) *PendingMail {
	p.locale = locale
	return p
}

// To adds recipients. The argument is a string, an [Address], a slice of
// either, or a map of address to name -- which is the "mixed" Illuminate takes.
//
// To is PendingMail::to, and Mailable::to.
func (p *PendingMail) To(users any, name ...string) *PendingMail {
	p.to = uniqueAddresses(append(p.to, addressesOf(users, name...)...))
	if p.locale == "" {
		if pref, ok := users.(interface{ PreferredLocale() string }); ok {
			p.Locale(pref.PreferredLocale())
		}
	}
	return p
}

// CC adds carbon copy recipients. Illuminate spells it cc.
//
// CC is PendingMail::cc, and Mailable::cc.
func (p *PendingMail) CC(users any, name ...string) *PendingMail {
	p.cc = uniqueAddresses(append(p.cc, addressesOf(users, name...)...))
	return p
}

// BCC adds blind carbon copy recipients. Illuminate spells it bcc.
//
// BCC is PendingMail::bcc, and Mailable::bcc.
func (p *PendingMail) BCC(users any, name ...string) *PendingMail {
	p.bcc = uniqueAddresses(append(p.bcc, addressesOf(users, name...)...))
	return p
}

// From sets the sender.
//
// From is Mailable::from.
func (p *PendingMail) From(address any, name ...string) *PendingMail {
	p.from = uniqueAddresses(append(p.from, addressesOf(address, name...)...))
	return p
}

// ReplyTo adds a reply-to address.
//
// ReplyTo is Mailable::replyTo.
func (p *PendingMail) ReplyTo(address any, name ...string) *PendingMail {
	p.replyTo = uniqueAddresses(append(p.replyTo, addressesOf(address, name...)...))
	return p
}

// Subject sets the subject. A mailable whose envelope names one wins over this.
//
// Subject is Mailable::subject.
func (p *PendingMail) Subject(subject string) *PendingMail {
	p.subject = subject
	return p
}

// View sets the HTML view and, optionally, what it renders from.
//
// View is Mailable::view.
func (p *PendingMail) View(view string, data ...any) *PendingMail {
	p.view = view
	return p.withData(data)
}

// HTML sets the message's HTML body directly, and is Illuminate's
// Mailable::html().
func (p *PendingMail) HTML(html string) *PendingMail {
	p.htmlString = html
	return p
}

// Text sets the plain-text view and, optionally, what it renders from.
//
// Text is Mailable::text.
func (p *PendingMail) Text(view string, data ...any) *PendingMail {
	p.textView = view
	return p.withData(data)
}

// Markdown sets the markdown view the message is drawn from. It produces both
// parts at once, which is the reason markdown mailables exist.
//
// Markdown is Mailable::markdown.
func (p *PendingMail) Markdown(view string, data ...any) *PendingMail {
	p.markdownView = view
	return p.withData(data)
}

// With adds view data. The key is a string with a value, or a map merged whole.
//
// With is Mailable::with.
func (p *PendingMail) With(key any, value ...any) *PendingMail {
	if p.viewData == nil {
		p.viewData = map[string]any{}
	}
	switch k := key.(type) {
	case map[string]any:
		for name, v := range k {
			p.viewData[name] = v
		}
	case string:
		if len(value) > 0 {
			p.viewData[k] = value[0]
		} else {
			p.viewData[k] = nil
		}
	}
	return p
}

func (p *PendingMail) withData(data []any) *PendingMail {
	if len(data) > 0 && data[0] != nil {
		if m, ok := data[0].(map[string]any); ok {
			return p.With(m)
		}
		return p.With("data", data[0])
	}
	return p
}

// Attach adds a file. The file is a path, an [*Attachment] or an [Attachable].
//
// Attach is Mailable::attach.
func (p *PendingMail) Attach(file any, options ...AttachOptions) *PendingMail {
	o := firstOption(options)

	if a, ok := file.(Attachable); ok {
		file = a.ToMailAttachment()
	}
	if a, ok := file.(*Attachment); ok {
		if err := a.AttachTo(p, o); err != nil && p.err == nil {
			p.err = err
		}
		return p
	}
	for _, existing := range p.attachments {
		if sameFile(existing.File, file) {
			return p
		}
	}
	p.attachments = append(p.attachments, AttachedFile{File: file, Options: o})
	return p
}

// AttachMany adds several files at once. See [Message.AttachMany] for the
// shapes an element may take.
//
// AttachMany is Mailable::attachMany.
func (p *PendingMail) AttachMany(files ...any) *PendingMail {
	for _, file := range files {
		if with, ok := file.(AttachedFile); ok {
			p.Attach(with.File, with.Options)
			continue
		}
		p.Attach(file)
	}
	return p
}

// AttachData adds bytes as an attachment under the given name.
//
// AttachData is Mailable::attachData.
func (p *PendingMail) AttachData(data []byte, name string, options ...AttachOptions) *PendingMail {
	o := firstOption(options)
	for _, existing := range p.rawAttachments {
		if existing.Name == name && string(existing.Data) == string(data) {
			return p
		}
	}
	p.rawAttachments = append(p.rawAttachments, AttachedData{Data: data, Name: name, Options: o})
	return p
}

// AttachFromStorage attaches a path on the default disk. An empty name means
// the path's last segment.
//
// AttachFromStorage is Mailable::attachFromStorage.
func (p *PendingMail) AttachFromStorage(path, name string, options ...AttachOptions) *PendingMail {
	return p.AttachFromStorageDisk("", path, name, options...)
}

// AttachFromStorageDisk attaches a path on the named disk, reading nothing
// until the message is built.
//
// AttachFromStorageDisk is Mailable::attachFromStorageDisk.
func (p *PendingMail) AttachFromStorageDisk(disk, path, name string, options ...AttachOptions) *PendingMail {
	msg := &Message{DiskAttachments: p.diskAttachments}
	msg.AttachFromStorageDisk(disk, path, name, options...)
	p.diskAttachments = msg.DiskAttachments
	return p
}

func (p *PendingMail) attachPath(path string, o AttachOptions) {
	p.attachments = append(p.attachments, AttachedFile{File: path, Options: o})
}

func (p *PendingMail) attachBytes(data []byte, name string, o AttachOptions) {
	p.rawAttachments = append(p.rawAttachments, AttachedData{Data: data, Name: name, Options: o})
}

// Tag adds a tag, for the providers that group by one.
//
// Tag is Mailable::tag.
func (p *PendingMail) Tag(value string) *PendingMail {
	p.tags = append(p.tags, value)
	return p
}

// Metadata adds a metadata pair, for the providers that carry them.
//
// Metadata is Mailable::metadata.
func (p *PendingMail) Metadata(key string, value ...string) *PendingMail {
	if p.metadata == nil {
		p.metadata = map[string]string{}
	}
	if len(value) > 0 {
		p.metadata[key] = value[0]
	} else {
		p.metadata[key] = ""
	}
	return p
}

// Priority sets the priority, where 1 is the highest and 5 the lowest.
// Illuminate defaults the level to 3 and so does an empty call here.
//
// Priority is Mailable::priority.
func (p *PendingMail) Priority(level ...int) *PendingMail {
	p.priority = 3
	if len(level) > 0 {
		p.priority = level[0]
	}
	return p
}

// Mailer names the mailer that should send this message, and is Illuminate's
// Mailable::mailer().
func (p *PendingMail) Mailer(name string) *PendingMail {
	p.mailerName = name
	return p
}

// Theme names the markdown theme this message is rendered with.
//
// Theme writes Mailable::$theme, which PHP sets as a property and
// Markdown::theme reads.
func (p *PendingMail) Theme(theme string) *PendingMail {
	p.theme = theme
	return p
}

// WithSymfonyMessage registers a callback that gets the message after it is
// built and before it is sent.
//
// Symfony in the name is Laravel's MIME library; the callback gets a [*Message]
// here, which is the same object under a different library's name.
//
// WithSymfonyMessage is Mailable::withSymfonyMessage.
func (p *PendingMail) WithSymfonyMessage(callback func(*Message)) *PendingMail {
	p.callbacks = append(p.callbacks, callback)
	return p
}

// BuildViewData is what the views render from.
//
// Illuminate reads the mailable's public properties by reflection and merges
// them with $viewData; this does the same with a struct's exported fields, so
// that a view can name a field of the mailable directly. It takes the mailable
// because in Go the mailable is not this object.
//
// BuildViewData is Mailable::buildViewData.
func (p *PendingMail) BuildViewData(mailable Mailable) any {
	content := mailable.Content()

	// The common case has nothing to merge: the mailable is the data, which is
	// what Illuminate's public-property reflection amounts to. Handing the
	// value straight through keeps a typed view typed.
	if len(p.viewData) == 0 && viewDataCallback == nil {
		if content.With != nil {
			return content.With
		}
		return mailable
	}

	data := map[string]any{}
	for name, value := range exportedFields(mailable) {
		data[name] = value
	}
	if m, ok := content.With.(map[string]any); ok {
		for name, value := range m {
			data[name] = value
		}
	} else if content.With != nil {
		data["with"] = content.With
	}
	if viewDataCallback != nil {
		for name, value := range viewDataCallback(mailable) {
			data[name] = value
		}
	}
	for name, value := range p.viewData {
		data[name] = value
	}
	return data
}

func exportedFields(v any) map[string]any {
	out := map[string]any{}
	value := reflect.ValueOf(v)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return out
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return out
	}
	t := value.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		if field.IsExported() {
			out[field.Name] = value.Field(i).Interface()
		}
	}
	return out
}

// Build turns the mailable into the message it will go out as.
//
// It is Illuminate's prepareMailableForDelivery() plus buildView() plus
// createMessage(), in the order that file runs them: the mailable's own
// envelope, content, attachments and headers are read, then the addresses
// collected here are merged in, then the views are rendered exactly once.
//
// Build is Mailable::prepareMailableForDelivery, which is protected there. It
// is exported here because a caller with no container needs a rendered message
// to assert against.
func (p *PendingMail) Build(ctx context.Context, mailable Mailable) (*Message, error) {
	if p.mailer == nil {
		return nil, errors.New("mail: this pending message has no mailer")
	}
	if p.err != nil {
		return nil, p.err
	}

	msg := p.mailer.createMessage()
	msg.Locale = p.locale
	if p.mailerName != "" {
		msg.mailerName = p.mailerName
	}
	env := mailable.Envelope()

	// Addressing. The pending addresses go first and the envelope's after, so
	// that a mailable that names a recipient adds to the ones the call site
	// gave rather than replacing them -- and the dedupe keeps the last
	// spelling, which is where a display name comes from.
	if len(p.from) > 0 {
		msg.From = p.from[len(p.from)-1]
	}
	if env.From.Address != "" {
		msg.From = env.From
	}
	msg.To = uniqueAddresses(append(append([]Address{}, p.to...), env.To...))
	msg.CC = uniqueAddresses(append(append([]Address{}, p.cc...), env.CC...))
	msg.BCC = uniqueAddresses(append(append([]Address{}, p.bcc...), env.BCC...))
	msg.ReplyTo = uniqueAddresses(append(append(msg.ReplyTo, p.replyTo...), env.ReplyTo...))

	msg.Subject = firstNonEmpty(env.Subject, p.subject)
	if msg.Subject == "" {
		msg.Subject = subjectFor(mailable)
	}

	msg.Tags = append(append([]string{}, p.tags...), env.Tags...)
	if len(p.metadata) > 0 || len(env.Metadata) > 0 {
		msg.Metadata = map[string]string{}
		for k, v := range p.metadata {
			msg.Metadata[k] = v
		}
		for k, v := range env.Metadata {
			msg.Metadata[k] = v
		}
	}
	if p.priority != 0 {
		msg.Priority(p.priority)
	}

	// Attachments, from here and from the mailable's own attachments().
	msg.Attachments = append(msg.Attachments, p.attachments...)
	msg.RawAttachments = append(msg.RawAttachments, p.rawAttachments...)
	msg.DiskAttachments = append(msg.DiskAttachments, p.diskAttachments...)
	if from, ok := mailable.(interface{ Attachments() []*Attachment }); ok {
		for _, a := range from.Attachments() {
			if err := a.AttachTo(msg); err != nil {
				return nil, err
			}
		}
	}

	// Headers, from the mailable's own headers().
	if from, ok := mailable.(interface{ Headers() Headers }); ok {
		headers := from.Headers()
		if msg.Headers.Text == nil {
			msg.Headers.Text = map[string]string{}
		}
		msg.Headers.MessageID = headers.MessageID
		msg.Headers.References = headers.References
		for k, v := range headers.Text {
			msg.Headers.Text[k] = v
		}
	}

	if err := p.renderInto(msg, mailable); err != nil {
		return nil, err
	}

	for _, callback := range append(append([]func(*Message){}, p.callbacks...), env.Using...) {
		if callback != nil {
			callback(msg)
		}
	}
	return msg, nil
}

func (p *PendingMail) renderInto(msg *Message, mailable Mailable) error {
	content := mailable.Content()
	data := p.BuildViewData(mailable)

	html := firstNonEmpty(content.View, content.HTML)
	html = firstNonEmpty(html, firstNonEmpty(p.view, p.htmlView))
	text := firstNonEmpty(content.Text, p.textView)
	markdown := firstNonEmpty(content.Markdown, p.markdownView)
	body := firstNonEmpty(content.HTMLString, p.htmlString)

	switch {
	case body != "":
		msg.HTML = body
	case markdown != "":
		if p.mailer.markdown == nil {
			return fmt.Errorf("mail: %q is a markdown view and no markdown renderer is wired", markdown)
		}
		md := p.mailer.markdown
		if p.theme != "" {
			md = md.clone().Theme(p.theme)
		}
		rendered, err := md.Render(markdown, data)
		if err != nil {
			return err
		}
		msg.HTML = rendered
		if text == "" {
			plain, err := md.RenderText(markdown, data)
			if err != nil {
				return err
			}
			msg.Text = plain
		}
	case html != "":
		rendered, err := p.mailer.renderView(html, data)
		if err != nil {
			return err
		}
		msg.HTML = rendered
	}

	if text != "" {
		rendered, err := p.mailer.renderView(text, data)
		if err != nil {
			return err
		}
		msg.Text = rendered
	}
	return nil
}

// Send builds the message and hands it to the transport.
//
// It is synchronous. Sending on the queue is [PendingMail.Queue], and that is
// deliberate: a call that sometimes blocks for two seconds and sometimes does
// not, decided by an interface the mailable implements somewhere else, is a
// call nobody can reason about from the line they are reading.
//
// Send is PendingMail::send.
func (p *PendingMail) Send(ctx context.Context, mailable Mailable) (SentMessage, error) {
	if p.mailer == nil {
		return SentMessage{}, errors.New("mail: this pending message has no mailer")
	}
	return p.mailer.sendMailable(ctx, mailable, p)
}

// SendNow sends without ever consulting the queue, and is Illuminate's
// PendingMail::sendNow().
func (p *PendingMail) SendNow(ctx context.Context, mailable Mailable) (SentMessage, error) {
	return p.Send(ctx, mailable)
}

// Render builds the message and answers its HTML part, without sending
// anything. It is Illuminate's Mailable::render().
func (p *PendingMail) Render(ctx context.Context, mailable Mailable) (string, error) {
	msg, err := p.Build(ctx, mailable)
	if err != nil {
		return "", err
	}
	return msg.HTML, nil
}

// Queue pushes the mailable onto the queue as a [SendQueuedMailable]. The queue
// name is optional, as it is in Illuminate.
//
// Queue is PendingMail::queue.
func (p *PendingMail) Queue(ctx context.Context, mailable Mailable, queue ...string) (string, error) {
	if p.mailer == nil || p.mailer.queue == nil {
		return "", ErrNoQueue
	}
	name := ""
	if len(queue) > 0 {
		name = queue[0]
	}
	return p.mailer.queue.Connection("").PushOn(ctx, name, p.newQueuedJob(mailable))
}

// Later pushes the mailable onto the queue, to be sent after the delay.
//
// Later is PendingMail::later.
func (p *PendingMail) Later(ctx context.Context, delay time.Duration, mailable Mailable, queue ...string) (string, error) {
	if p.mailer == nil || p.mailer.queue == nil {
		return "", ErrNoQueue
	}
	name := ""
	if len(queue) > 0 {
		name = queue[0]
	}
	job := p.newQueuedJob(mailable)
	job.Delay = delay
	return p.mailer.queue.Connection("").LaterOn(ctx, name, delay, job)
}

func (p *PendingMail) newQueuedJob(mailable Mailable) *SendQueuedMailable {
	return &SendQueuedMailable{Mailable: mailable, Pending: p}
}
