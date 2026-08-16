package mail

import (
	"bytes"
	"errors"
	"fmt"
	"path"
)

// AttachOptions is what an attachment goes under: its name in the message and
// its content type. It arrives as a variadic tail everywhere it is taken,
// because both halves have a sensible default.
type AttachOptions struct {
	// As is what the file is called in the message.
	As string
	// Mime is the content type. Empty means it is guessed from the name.
	Mime string
}

func firstOption(options []AttachOptions) AttachOptions {
	if len(options) > 0 {
		return options[0]
	}
	return AttachOptions{}
}

// UploadedFile is the part of an uploaded file that [FromUploadedFile] needs.
// It is an interface rather than an import because an uploaded file belongs to
// the HTTP layer and this package is underneath it.
type UploadedFile interface {
	GetClientOriginalName() string
	GetMimeType() string
	GetClientMimeType() string
	Get() ([]byte, error)
}

// Attachment is a file on its way into a message, described once and resolved
// when somebody actually attaches it.
//
// The indirection earns its keep: [FromStorageDisk] names a disk and a path
// without reading anything, so a mailable can be built, queued, serialised and
// only then touch the filesystem -- on the worker, where the disk is
// configured, rather than in the request that built it.
//
// The name and the content type are unexported, and [Attachment.As] and
// [Attachment.WithMime] are how they are set: a Go type cannot carry a method
// and a field of the same name, and the fluent form is what the constructors
// chain into.
type Attachment struct {
	as   string
	mime string

	// resolver hands the attachment back to one of the two strategies, which is
	// how the same attachment can be turned into a path for one caller and into
	// bytes for another.
	resolver func(a *Attachment, pathStrategy func(string) any, dataStrategy func(func() ([]byte, error)) any) any
}

// FromPath is an attachment read from a path when it is resolved.
func FromPath(p string) *Attachment {
	return &Attachment{
		resolver: func(a *Attachment, pathStrategy func(string) any, _ func(func() ([]byte, error)) any) any {
			return pathStrategy(p)
		},
	}
}

// FromURL is an attachment named by URL. It is [FromPath], because a URL is a
// path as far as the resolver is concerned.
func FromURL(url string) *Attachment { return FromPath(url) }

// FromData is an attachment made of bytes produced on demand, under a name. The
// name is optional here and required by the time it is attached: see
// [ErrAttachmentNeedsName].
func FromData(data func() ([]byte, error), name ...string) *Attachment {
	a := &Attachment{
		resolver: func(a *Attachment, _ func(string) any, dataStrategy func(func() ([]byte, error)) any) any {
			return dataStrategy(data)
		},
	}
	if len(name) > 0 {
		a.As(name[0])
	}
	return a
}

// FromUploadedFile is an attachment made from an upload. The name and the
// content type come off the upload, which is what stops a browser-supplied
// filename from being the only thing describing the part.
func FromUploadedFile(file UploadedFile) *Attachment {
	return &Attachment{
		resolver: func(a *Attachment, _ func(string) any, dataStrategy func(func() ([]byte, error)) any) any {
			mime := file.GetMimeType()
			if mime == "" {
				mime = file.GetClientMimeType()
			}
			a.As(file.GetClientOriginalName()).WithMime(mime)
			return dataStrategy(file.Get)
		},
	}
}

// FromStorage is an attachment at a path on the default disk.
func FromStorage(p string) *Attachment { return FromStorageDisk("", p) }

// FromStorageDisk is an attachment at a path on the named disk.
//
// Nothing is read here. The disk is asked for the bytes and the content type at
// the moment the attachment is resolved, which is why a queued mailable can
// carry one across a process boundary.
func FromStorageDisk(disk, p string) *Attachment {
	return &Attachment{
		resolver: func(a *Attachment, _ func(string) any, dataStrategy func(func() ([]byte, error)) any) any {
			if a.as == "" {
				a.As(path.Base(p))
			}
			if a.mime == "" && Filesystem != nil {
				if mime, err := Filesystem.Disk(disk).MimeType(p); err == nil {
					a.WithMime(mime)
				}
			}
			return dataStrategy(func() ([]byte, error) {
				if Filesystem == nil {
					return nil, fmt.Errorf("%w: attaching %q", ErrNoFilesystem, p)
				}
				return Filesystem.Disk(disk).Get(p)
			})
		},
	}
}

// FromCloudStorage is an attachment at a path on whatever disk [CloudDisk]
// names.
func FromCloudStorage(p string) *Attachment { return FromStorageDisk(CloudDisk, p) }

// As sets what the file is called in the message.
func (a *Attachment) As(name string) *Attachment {
	a.as = name
	return a
}

// WithMime sets the content type.
func (a *Attachment) WithMime(mime string) *Attachment {
	a.mime = mime
	return a
}

// AttachWith runs the attachment through whichever of the two strategies it was
// built for.
//
// It is the one method every other one on this type is written in terms of: a
// path attachment reaches pathStrategy, a data attachment reaches dataStrategy,
// and neither caller has to ask which kind it is holding.
func (a *Attachment) AttachWith(
	pathStrategy func(path string) any,
	dataStrategy func(data func() ([]byte, error)) any,
) any {
	if a == nil || a.resolver == nil {
		return nil
	}
	return a.resolver(a, pathStrategy, dataStrategy)
}

// attachTarget is what AttachTo can attach to: anything that takes a path or
// takes bytes, which is [Message] and [PendingMail].
type attachTarget interface {
	attachPath(path string, o AttachOptions)
	attachBytes(data []byte, name string, o AttachOptions)
}

// ErrAttachmentNeedsName is returned by [Attachment.AttachTo] when an
// attachment made of bytes has no filename. An attachment built from a path
// can always fall back to the base name of that path; one built from data --
// [FromData] with no name, or a generated report -- has nothing to fall back
// to, and a part with no filename is a part the recipient's client shows as
// untitled.
//
// The caller fixes it at the point of building: As on the attachment, or the
// As field of the [AttachOptions] passed to AttachTo.
var ErrAttachmentNeedsName = errors.New("mail: an attachment built from data needs a filename")

// AttachTo attaches this attachment to a message.
//
// It returns an error rather than panicking when data has no name to go under,
// so the caller finds out at the call.
func (a *Attachment) AttachTo(target any, options ...AttachOptions) error {
	to, ok := target.(attachTarget)
	if !ok {
		return fmt.Errorf("mail: cannot attach to %T", target)
	}
	o := firstOption(options)

	var failed error
	a.AttachWith(
		func(p string) any {
			to.attachPath(p, AttachOptions{
				As:   firstNonEmpty(o.As, a.as),
				Mime: firstNonEmpty(o.Mime, a.mime),
			})
			return nil
		},
		func(data func() ([]byte, error)) any {
			resolved := AttachOptions{
				As:   firstNonEmpty(o.As, a.as),
				Mime: firstNonEmpty(o.Mime, a.mime),
			}
			if resolved.As == "" {
				failed = ErrAttachmentNeedsName
				return nil
			}
			bytes, err := data()
			if err != nil {
				failed = err
				return nil
			}
			to.attachBytes(bytes, resolved.As, AttachOptions{Mime: resolved.Mime})
			return nil
		},
	)
	return failed
}

// resolved is what an attachment turns into once somebody insists: either a
// path or the bytes, plus the name and type it goes under.
type resolved struct {
	Path string
	Data []byte
	As   string
	Mime string
}

func (a *Attachment) resolve(o AttachOptions) resolved {
	out := resolved{
		As:   firstNonEmpty(o.As, a.as),
		Mime: firstNonEmpty(o.Mime, a.mime),
	}
	a.AttachWith(
		func(p string) any { out.Path = p; return nil },
		func(data func() ([]byte, error)) any {
			b, err := data()
			if err == nil {
				out.Data = b
			}
			// The name and type may have been set by the resolver itself --
			// FromStorageDisk and FromUploadedFile both do that -- so they are
			// read back after it ran, not before.
			out.As = firstNonEmpty(o.As, a.as)
			out.Mime = firstNonEmpty(o.Mime, a.mime)
			return nil
		},
	)
	return out
}

// IsEquivalent reports whether the two attachments would produce the same part.
//
// It is what makes [Message.AssertHasAttachment] work against an attachment the
// mailable declared rather than one added by hand: two descriptions of the same
// file are the same attachment even though they are not the same value.
func (a *Attachment) IsEquivalent(other *Attachment, options ...AttachOptions) bool {
	if a == nil || other == nil {
		return a == other
	}
	mine := a.resolve(AttachOptions{})
	theirs := other.resolve(firstOption(options))

	return mine.Path == theirs.Path &&
		bytes.Equal(mine.Data, theirs.Data) &&
		mine.As == theirs.As &&
		mine.Mime == theirs.Mime
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
