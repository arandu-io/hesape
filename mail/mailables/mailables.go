// Package mailables is the value types a mailable declares: the address, the
// attachment, the content, the envelope and the headers.
//
// # Why every name here is an alias
//
// The five types and the mailer that uses them reference each other: a Mailer
// takes an Envelope, and an Attachment is built and attached by the same
// package that sends. Go refuses that cycle, and one of the two directions has
// to give way.
//
// So the types live in [github.com/arandu-io/hesape/mail], next to the values
// that use them, and this package names them again. An alias is not a second
// type:
//
//	func (m OrderShipped) Envelope() mailables.Envelope {
//		return mailables.Envelope{Subject: "Your order has shipped"}
//	}
//
// is the same declaration as one written against mail.Envelope, and the two
// interoperate because they are one type.
package mailables

import "github.com/arandu-io/hesape/mail"

// The five value types a mailable declares.
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
	// AttachOptions is what an attachment goes under: its name and its type.
	AttachOptions = mail.AttachOptions
)

// The constructors of those types, under this package's name.
var (
	// NewAddress builds an address from a mailbox and an optional name.
	NewAddress = mail.NewAddress
	// NewEnvelope builds an envelope from addresses given as strings or values.
	NewEnvelope = mail.NewEnvelope

	// FromPath is an attachment read from a path.
	FromPath = mail.FromPath
	// FromURL is an attachment named by URL.
	FromURL = mail.FromURL
	// FromData is an attachment made of bytes produced on demand.
	FromData = mail.FromData
	// FromUploadedFile is an attachment made from an upload.
	FromUploadedFile = mail.FromUploadedFile
	// FromStorage is an attachment at a path on the default disk.
	FromStorage = mail.FromStorage
	// FromStorageDisk is an attachment at a path on the named disk.
	FromStorageDisk = mail.FromStorageDisk
	// FromCloudStorage is an attachment at a path on the cloud disk.
	FromCloudStorage = mail.FromCloudStorage
)
