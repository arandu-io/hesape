package filesystem

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path"
	"strings"
)

// ErrRefusedUpload is what every [UploadRules] failure unwraps to, so a handler
// answers "we cannot take this file" once instead of switching on four reasons.
var ErrRefusedUpload = errors.New("filesystem: the upload was refused")

// Upload is a file that arrived on a request.
//
// It is the "own contract" that validation points at: a file is not in
// url.Values, and validating one inside the form pipeline would need a second
// input shape and a second set of rules, which is a second way to validate
// (RULE 9). An upload is checked here, against [UploadRules], and stored with
// [Disk.Put].
//
// Everything on it except Size came from the client and is a string somebody
// chose. Name is not a key: it is what to call the file if it is ever offered
// back, and using it as a key is how "../../etc/passwd" gets stored -- which
// [CleanKey] then refuses, but the habit is the bug. Size is counted by the
// server as the body is read, so it is the one field worth checking.
type Upload struct {
	// Field is the form field the file arrived in.
	Field string
	// Name is the filename the client announced, with any directory stripped.
	Name string
	// Size is how many bytes arrived.
	Size int64
	// ContentType is the type the client announced. It is recorded and never
	// trusted: [Disk.Put] infers the stored type from the key instead.
	ContentType string
	// Open reads the content. The caller closes what it returns, and may call
	// Open more than once.
	Open func() (io.ReadCloser, error)
}

// FromMultipart builds an Upload out of a parsed multipart part.
//
// It is the one place multipart is understood, so the HTTP layer above hands
// over a *multipart.FileHeader and gets back something with rules attached.
func FromMultipart(field string, h *multipart.FileHeader) Upload {
	return Upload{
		Field: field,
		// A browser sends a bare filename and a script sends whatever it likes.
		// path.Base and the backslash trim between them leave nothing that
		// reads as a directory on either kind of host.
		Name:        path.Base(strings.ReplaceAll(h.Filename, `\`, "/")),
		Size:        h.Size,
		ContentType: h.Header.Get("Content-Type"),
		Open: func() (io.ReadCloser, error) {
			f, err := h.Open()
			if err != nil {
				return nil, fmt.Errorf("filesystem: opening upload %q: %w", h.Filename, err)
			}
			return f, nil
		},
	}
}

// Extension returns the lowercased extension of the announced name, with the
// dot, or "" when there is none.
func (u Upload) Extension() string { return strings.ToLower(path.Ext(u.Name)) }

// UploadRules is what an upload has to satisfy.
//
// Both fields are required, and both fail closed. A rules value that named no
// maximum would accept a four gigabyte file, and one that named no extension
// would accept an .exe -- and the moment those are the defaults, the rule that
// was forgotten looks exactly like the rule that was written.
//
// There is no content-type rule. The announced type is a header the client
// wrote, so checking it stops nobody who is trying; the extension is checked
// because it is what a person sees, and the real defense is on the way out --
// the stored type comes from the key, and [Send] sends nosniff.
type UploadRules struct {
	// MaxBytes is the largest file accepted. It must be positive.
	MaxBytes int64
	// Extensions is the accepted set, lowercased and with the dot: ".pdf".
	// It must not be empty.
	Extensions []string
}

// Check answers whether this upload satisfies the rules.
//
// Every failure unwraps to [ErrRefusedUpload] and every message is one a person
// can act on, because most of them are shown to one.
func (u Upload) Check(r UploadRules) error {
	if r.MaxBytes <= 0 {
		return fmt.Errorf("%w: UploadRules.MaxBytes is not set, and a size limit that defaults to unlimited is the one nobody notices is missing", ErrRefusedUpload)
	}
	if len(r.Extensions) == 0 {
		return fmt.Errorf("%w: UploadRules.Extensions is empty, and an accepted set that defaults to everything accepts an executable", ErrRefusedUpload)
	}
	if u.Open == nil {
		return fmt.Errorf("%w: it carries no content", ErrRefusedUpload)
	}
	if u.Size <= 0 {
		return fmt.Errorf("%w: the file is empty", ErrRefusedUpload)
	}
	if u.Size > r.MaxBytes {
		return fmt.Errorf("%w: it is %d bytes and the limit is %d", ErrRefusedUpload, u.Size, r.MaxBytes)
	}
	ext := u.Extension()
	for _, want := range r.Extensions {
		if ext == strings.ToLower(want) {
			return nil
		}
	}
	if ext == "" {
		return fmt.Errorf("%w: %q has no extension, and only %s are accepted", ErrRefusedUpload, u.Name, strings.Join(r.Extensions, ", "))
	}
	return fmt.Errorf("%w: %s is not accepted, only %s", ErrRefusedUpload, ext, strings.Join(r.Extensions, ", "))
}
