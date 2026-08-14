package mail

import (
	"context"
	"errors"
	"time"
)

// ErrNoRecipient is returned by Send when nobody was addressed. It is an error
// rather than a silent no-op: a message with no recipient is a message somebody
// meant to send.
var ErrNoRecipient = errors.New("mail: no recipient")

// ErrRetryable marks a failure worth trying again.
//
// A 429 or a 5xx from a provider is not the same event as a rejected address,
// and treating them alike is how a verification e-mail is silently lost during a
// rate limit. A job that sends checks for this and reschedules; a request that
// sends inline reports it and moves on.
//
//	var retryable mail.ErrRetryable
//	if errors.As(err, &retryable) { ... }
//
// It lives here rather than with the transports because the caller that acts on
// it -- the job, the handler -- already imports this package and has no reason
// to import a transport it does not name.
type ErrRetryable struct{ err error }

// Error has no PHP counterpart: the retryable half of a delivery failure is
// Symfony's TransportException hierarchy, which NotificationSender catches by
// class rather than by a marker on the error.
func (e ErrRetryable) Error() string { return e.err.Error() }

// Unwrap has no PHP counterpart, for the reason [ErrRetryable.Error] gives.
func (e ErrRetryable) Unwrap() error { return e.err }

// Retryable marks err as worth trying again, and is how a transport outside
// this package builds an [ErrRetryable]. It returns nil for a nil error, so it
// can wrap a result without a check around it.
//
// Retryable has no PHP counterpart, for the reason ErrRetryable.Error gives.
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return ErrRetryable{err}
}

// Mailable is anything that knows how to describe itself as a message.
//
// It stands where Illuminate\Contracts\Mail\Mailable stands. The class
// Illuminate\Mail\Mailable has no Go twin, because its whole design is
// inheritance: a user class extends it and overrides envelope(), content(),
// attachments(), headers() and build(), and the parent calls back into the
// child. Go has no virtual dispatch through an embedded struct, so the two
// methods a mailable must answer are an interface, and the fluent surface the
// PHP base class carries lives on [PendingMail], which is the value that is
// doing the sending. Every one of those method names is Illuminate's.
//
// Attachments and Headers are optional: a type that declares
//
//	Attachments() []*Attachment
//	Headers() Headers
//
// has them read the way Illuminate reads the methods of the same names, and a
// type that does not declare them is not asked.
type Mailable interface {
	Envelope() Envelope
	Content() Content
}

// Attachable is Illuminate\Contracts\Mail\Attachable: a domain type that knows
// how to turn itself into an attachment, so that Attach can be handed an
// invoice rather than a path.
type Attachable interface {
	ToMailAttachment() *Attachment
}

// SentMessage is Illuminate\Mail\SentMessage: what a Transport reports back
// about a message it accepted.
//
// It exists because the provider's identifier was being discarded. That
// identifier is the only thing joining a line in the application log to a row in
// the provider's dashboard, and without it "did this person get the e-mail?" has
// no answer that is not a search by address and a guess at the timestamp.
type SentMessage struct {
	// ID is what the provider called the message.
	//
	// Empty when the transport has no such notion: the log and array transports
	// do not, and SMTP only learns an identifier if the receiving server
	// volunteers one in its reply. An empty ID is not a failure.
	ID string

	// Transport is the Name of the transport that accepted the message. It is
	// recorded here rather than assumed by the caller because a failover
	// transport delivers through whichever of its own accepted the message.
	Transport string

	// Message is what was sent, which is what Illuminate's
	// SentMessage::getOriginalMessage() answers. It is nil when a transport
	// built the receipt without one.
	Message *Message
}

// GetSymfonySentMessage is Illuminate's SentMessage::getSymfonySentMessage().
//
// Symfony in the name is Laravel's mailer library; hesape has none, so what it
// returns is this value itself -- the receipt is not a wrapper here, it is the
// thing. The name is kept because it is the name a Laravel developer reaches
// for (ADR 0044).
func (s SentMessage) GetSymfonySentMessage() SentMessage { return s }

// Transport delivers a rendered message.
//
// One method, so writing one is small: an adapter for a provider is a POST and
// an error, and everything above it -- addressing, rendering, validation -- has
// already happened.
type Transport interface {
	Send(ctx context.Context, m Message) (SentMessage, error)
	// Name is what appears in a log line and on the debug console.
	Name() string
}

// Renderer draws the view a Content names.
//
// It stands where Illuminate\Contracts\View\Factory stands in the Mailer's
// constructor. An interface here rather than the view package directly, because
// mail is imported by the modules that send and importing the view package from
// all of them would put the whole view registry behind every one. The
// collection's view package satisfies it.
type Renderer interface {
	RenderToString(name string, data any) (string, error)
}

// Dispatcher is the slice of Illuminate\Contracts\Events\Dispatcher the Mailer
// uses: one call that can veto, and one that only announces.
type Dispatcher interface {
	// Until reports whether the send should go ahead. It is Illuminate's
	// $events->until($event) !== false: a listener that refuses stops the
	// message, and no listener at all lets it through.
	Until(ctx context.Context, event any) bool
	// Dispatch announces an event nobody can refuse.
	Dispatch(ctx context.Context, event any)
}

// FilesystemFactory is the slice of Illuminate\Contracts\Filesystem\Factory
// that attachments need.
//
// It is declared here rather than imported because hesape/filesystem is being
// written by another workflow; when it lands, its factory satisfies this and
// this declaration goes away.
type FilesystemFactory interface {
	// Disk is the named disk, or the default one when name is empty.
	Disk(name string) Disk
}

// Disk is the slice of Illuminate\Contracts\Filesystem\Filesystem that
// attachments need: the bytes, and what to call them.
type Disk interface {
	Get(path string) ([]byte, error)
	MimeType(path string) (string, error)
}

// Filesystem is where fromStorage, fromStorageDisk and attachFromStorage find
// their disks.
//
// Illuminate resolves the factory out of the container. ADR 0001 has no
// container, so an application wires this once at boot and the attachment
// constructors read it. Nil means an attachment from storage fails with an
// error that says so rather than panicking three layers down.
var Filesystem FilesystemFactory

// CloudDisk is the name Illuminate's Storage::getDefaultCloudDriver() answers,
// and is what Attachment.FromCloudStorage asks Filesystem for.
var CloudDisk = "s3"

// QueueFactory is the slice of Illuminate\Contracts\Queue\Factory that queueing
// a mailable needs.
//
// It is declared here rather than imported because hesape/queue is being
// written by another workflow.
type QueueFactory interface {
	// Connection is the named connection, or the default one when name is empty.
	Connection(name string) Queue
}

// Queue is the slice of Illuminate\Contracts\Queue\Queue that queueing a
// mailable needs: push now, and push later.
type Queue interface {
	PushOn(ctx context.Context, queue string, job any) (string, error)
	LaterOn(ctx context.Context, queue string, delay time.Duration, job any) (string, error)
}

// ErrNoQueue is what Queue and Later return when no queue was wired. Illuminate
// would have resolved one from the container and thrown if it could not; the
// error carries the same fact without the container.
var ErrNoQueue = errors.New("mail: no queue connection is wired")

// ErrNoFilesystem is what an attachment from storage returns when [Filesystem]
// was never set.
var ErrNoFilesystem = errors.New("mail: no filesystem factory is wired")
