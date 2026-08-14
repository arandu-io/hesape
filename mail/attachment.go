package mail

import (
	"bytes"
	"errors"
	"fmt"
	"path"
)

// AttachOptions is Illuminate's ['as' => ..., 'mime' => ...] options array,
// which is the one place this component uses an untyped PHP array as a record.
// A struct is what that array is, and it arrives as a variadic tail because
// Illuminate's parameter has a default.
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

// UploadedFile is the slice of Illuminate\Http\UploadedFile that
// Attachment.FromUploadedFile needs. It is an interface rather than an import
// because an uploaded file belongs to the HTTP layer and this package is
// underneath it.
type UploadedFile interface {
	GetClientOriginalName() string
	GetMimeType() string
	GetClientMimeType() string
	Get() ([]byte, error)
}

// Attachment is Illuminate\Mail\Attachment: a file on its way into a message,
// described once and resolved when somebody actually attaches it.
//
// The indirection is Illuminate's and it earns its keep: FromStorageDisk names
// a disk and a path without reading anything, so a mailable can be built,
// queued, serialised and only then touch the filesystem -- on the worker, where
// the disk is configured, rather than in the request that built it.
//
// Illuminate's $as and $mime are public properties with fluent setters of the
// same name, which PHP allows and Go does not. Here the setters win, because
// As() and WithMime() are how Illuminate's own examples read.
type Attachment struct {
	as   string
	mime string

	// resolver is Illuminate's $resolver closure. It hands itself back to one
	// of the two strategies, which is how the same attachment can be turned
	// into a path for one caller and into bytes for another.
	resolver func(a *Attachment, pathStrategy func(string) any, dataStrategy func(func() ([]byte, error)) any) any
}

// FromPath is Illuminate's Attachment::fromPath(). Static constructors have no
// Go twin, so they are package-level functions carrying the same name.
func FromPath(p string) *Attachment {
	return &Attachment{
		resolver: func(a *Attachment, pathStrategy func(string) any, _ func(func() ([]byte, error)) any) any {
			return pathStrategy(p)
		},
	}
}

// FromURL is Illuminate's Attachment::fromUrl(). Go spells the initialism in
// capitals (ADR 0044); the behaviour is fromPath's, because a URL is a path as
// far as the resolver is concerned.
func FromURL(url string) *Attachment { return FromPath(url) }

// FromData is Illuminate's Attachment::fromData(): bytes produced on demand,
// under a name. The name is optional in PHP and arrives as a variadic tail.
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

// FromUploadedFile is Illuminate's Attachment::fromUploadedFile(). The name and
// the content type come off the upload, which is what stops a browser-supplied
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

// FromStorage is Illuminate's Attachment::fromStorage(): a path on the default
// disk.
func FromStorage(p string) *Attachment { return FromStorageDisk("", p) }

// FromStorageDisk is Illuminate's Attachment::fromStorageDisk().
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

// FromCloudStorage is Illuminate's Attachment::fromCloudStorage(): a path on
// whatever disk [CloudDisk] names.
func FromCloudStorage(p string) *Attachment { return FromStorageDisk(CloudDisk, p) }

// As sets what the file is called in the message, and is Illuminate's
// Attachment::as().
func (a *Attachment) As(name string) *Attachment {
	a.as = name
	return a
}

// WithMime sets the content type.
//
// WithMime is Attachment::withMime.
func (a *Attachment) WithMime(mime string) *Attachment {
	a.mime = mime
	return a
}

// AttachWith runs the attachment through whichever of the two strategies it was
// built for, and is Illuminate's Attachment::attachWith().
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

// attachTarget is what AttachTo can attach to. Illuminate type-hints
// Mailable|Message|MailMessage there; the two shapes those three share are
// attach-a-path and attach-bytes, so that is the interface.
type attachTarget interface {
	attachPath(path string, o AttachOptions)
	attachBytes(data []byte, name string, o AttachOptions)
}

// ErrAttachmentNeedsName is Illuminate's
// RuntimeException('Attachment requires a filename to be specified.').
var ErrAttachmentNeedsName = errors.New("mail: an attachment built from data needs a filename")

// AttachTo attaches this attachment to a message, and is Illuminate's
// Attachment::attachTo().
//
// PHP throws when data has no name to go under; ADR 0044 turns a throw into an
// error, so the caller finds out at the call rather than at the panic.
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
			// fromStorageDisk and fromUploadedFile both do that -- so they are
			// read back after it ran, not before.
			out.As = firstNonEmpty(o.As, a.as)
			out.Mime = firstNonEmpty(o.Mime, a.mime)
			return nil
		},
	)
	return out
}

// IsEquivalent reports whether the two attachments would produce the same part,
// and is Illuminate's Attachment::isEquivalent().
//
// It is what makes assertHasAttachment work against an attachment declared in
// the mailable's attachments() rather than added by hand: two descriptions of
// the same file are the same attachment even though they are not the same
// value.
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
