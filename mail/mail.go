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

// Error is the wrapped failure's message: the marker adds no text of its own.
func (e ErrRetryable) Error() string { return e.err.Error() }

// Unwrap is the failure underneath the marker, so errors.Is and errors.As reach
// whatever the transport returned.
func (e ErrRetryable) Unwrap() error { return e.err }

// Retryable marks err as worth trying again, and is how a transport outside
// this package builds an [ErrRetryable]. It returns nil for a nil error, so it
// can wrap a result without a check around it.
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return ErrRetryable{err}
}

// Mailable is anything that knows how to describe itself as a message.
//
// It is two methods rather than a base type to embed, because Go has no virtual
// dispatch through an embedded struct: there would be no way for the parent to
// call back into the child. The fluent surface lives on [PendingMail] instead,
// which is the value that is doing the sending.
//
// Attachments and Headers are optional: a type that declares
//
//	Attachments() []*Attachment
//	Headers() Headers
//
// has them read when the message is built, and a type that does not declare
// them is not asked.
type Mailable interface {
	Envelope() Envelope
	Content() Content
}

// Attachable is a domain type that knows how to turn itself into an attachment,
// so that Attach can be handed an invoice rather than a path.
type Attachable interface {
	ToMailAttachment() *Attachment
}

// SentMessage is what a Transport reports back about a message it accepted.
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

	// Message is what was sent. It is nil when a transport built the receipt
	// without one.
	Message *Message
}

// GetSymfonySentMessage is the receipt the mail library produced, which is this
// value: it is not a wrapper around one, so the method returns the receiver.
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
// It is an interface here rather than the view package directly, because mail
// is imported by the modules that send and importing the view package from all
// of them would put the whole view registry behind every one.
type Renderer interface {
	RenderToString(name string, data any) (string, error)
}

// Dispatcher is how a [Mailer] tells the application that a message is about to
// go out, and that one went out. Two calls, because the two moments differ in
// what a listener is allowed to do: before sending, a listener can refuse and
// the message is dropped; after sending, the message is already gone and there
// is nothing to refuse.
//
// It is the whole of the event system this package needs. A Mailer with no
// dispatcher sends every message and announces nothing, so wiring one is
// optional.
type Dispatcher interface {
	// Until reports whether the send should go ahead: a listener that refuses
	// stops the message, and no listener at all lets it through.
	Until(ctx context.Context, event any) bool
	// Dispatch announces an event nobody can refuse.
	Dispatch(ctx context.Context, event any)
}

// FilesystemFactory is the part of a filesystem that attachments need: one call
// for a named disk.
//
// It is declared here rather than imported so that a module which only sends
// mail does not pull a storage registry in behind it.
type FilesystemFactory interface {
	// Disk is the named disk, or the default one when name is empty.
	Disk(name string) Disk
}

// Disk is one place files are stored, as an attachment sees it: the two
// questions [FromStorageDisk] and [Message.AttachFromStorage] ask of a stored
// file, which are its bytes and its content type. Nothing about writing,
// listing or deleting appears here, because building a message never does any
// of that.
//
// Both are asked late -- when the attachment is resolved, not when it is
// declared -- so a mailable that names a path can be built in a request and
// read on a worker.
type Disk interface {
	Get(path string) ([]byte, error)
	MimeType(path string) (string, error)
}

// Filesystem is where [FromStorage], [FromStorageDisk] and
// [Message.AttachFromStorage] find their disks.
//
// An application sets it once at boot and the attachment constructors read it.
// Nil means an attachment from storage fails with [ErrNoFilesystem] rather than
// panicking three layers down.
var Filesystem FilesystemFactory

// CloudDisk is the disk name [FromCloudStorage] asks [Filesystem] for.
var CloudDisk = "s3"

// QueueFactory is the part of a queue that sending a mailable in the background
// needs: one call for a named connection.
//
// It is declared here rather than imported so that a module which only sends
// mail does not pull a queue registry in behind it.
type QueueFactory interface {
	// Connection is the named connection, or the default one when name is empty.
	Connection(name string) Queue
}

// Queue is one queue connection, as a mailable being sent in the background
// sees it: hand a job over to be run as soon as a worker is free, or after a
// delay. [PendingMail.Queue] uses the first, [PendingMail.Later] the second,
// and both return whatever identifier the connection gave the job.
//
// The queue name is a parameter rather than part of the connection because a
// single connection carries many, and which one a mailable goes on is decided
// at the call.
type Queue interface {
	PushOn(ctx context.Context, queue string, job any) (string, error)
	LaterOn(ctx context.Context, queue string, delay time.Duration, job any) (string, error)
}

// ErrNoQueue is what Queue and Later return when no queue was wired.
var ErrNoQueue = errors.New("mail: no queue connection is wired")

// ErrNoFilesystem is what an attachment from storage returns when [Filesystem]
// was never set.
var ErrNoFilesystem = errors.New("mail: no filesystem factory is wired")
