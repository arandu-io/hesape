// Package mailables mirrors Illuminate\Mail\Mailables.
//
// The files it answers to, in the clone at laravel_illuminate/mail/Mailables:
//
//	Address.php
//	Attachment.php
//	Content.php
//	Envelope.php
//	Headers.php
//
// # Why every name here is an alias
//
// Illuminate\Mail and Illuminate\Mail\Mailables reference each other: Mailer
// and Mailable take a Mailables\Address, and Mailables\Attachment extends
// Illuminate\Mail\Attachment. PHP namespaces allow that; Go packages do not,
// and one of the two directions has to give way.
//
// So the five types live in [github.com/arandu-io/hesape/mail], where the
// classes that use them already are, and this package is the import path a
// Laravel developer reaches for. An alias is not a second type -- these are the
// same types under a second name, which is what Illuminate's own
// Mailables\Attachment is: an empty subclass with the comment "Here for
// namespace consistency".
//
//	func (m OrderShipped) Envelope() mailables.Envelope {
//		return mailables.Envelope{Subject: "Your order has shipped"}
//	}
//
// is the same declaration as one written against mail.Envelope, and the two
// interoperate because they are one type.
package mailables

import "github.com/arandu-io/hesape/mail"

// The five types of Illuminate\Mail\Mailables.
type (
	// Address is one mailbox, with the display name that goes in front of it.
	Address = mail.Address
	// Attachment is a file on its way into a message.
	Attachment = mail.Attachment
	// Content is what the body is made of.
	Content = mail.Content
	// Envelope is who a message is from, who it is to, and what it says it is.
	Envelope = mail.Envelope
	// Headers is the three header fields a mailable may set by hand.
	Headers = mail.Headers
	// AttachOptions is Illuminate's ['as' => ..., 'mime' => ...].
	AttachOptions = mail.AttachOptions
)

// The constructors of those types. Go has no static methods, so Illuminate's
// Attachment::fromPath() and friends are package-level functions, and these are
// the same functions under this package's name.
var (
	// NewAddress is Illuminate's Address::__construct.
	NewAddress = mail.NewAddress
	// NewEnvelope is Illuminate's Envelope::__construct.
	NewEnvelope = mail.NewEnvelope

	// FromPath is Illuminate's Attachment::fromPath.
	FromPath = mail.FromPath
	// FromURL is Illuminate's Attachment::fromUrl.
	FromURL = mail.FromURL
	// FromData is Illuminate's Attachment::fromData.
	FromData = mail.FromData
	// FromUploadedFile is Illuminate's Attachment::fromUploadedFile.
	FromUploadedFile = mail.FromUploadedFile
	// FromStorage is Illuminate's Attachment::fromStorage.
	FromStorage = mail.FromStorage
	// FromStorageDisk is Illuminate's Attachment::fromStorageDisk.
	FromStorageDisk = mail.FromStorageDisk
	// FromCloudStorage is Illuminate's Attachment::fromCloudStorage.
	FromCloudStorage = mail.FromCloudStorage
)
