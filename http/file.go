package http

import (
	"fmt"
	stdhttp "net/http"

	"github.com/arandu-io/hesape/filesystem"
)

// File is the one file that arrived in a form field.
//
// It is the whole of what this package knows about multipart, and it exists so
// that the knowing happens once: hesape/filesystem.FromMultipart is waiting for
// a *multipart.FileHeader and had nothing handing it one, so every project would
// have written this call itself, and the ones that wrote it slightly differently
// would have differed on what a filename is.
//
// It is a function taking a Context rather than a method for the reason
// [filesystem.Upload] is where it is: an upload is checked against
// [filesystem.UploadRules] and stored with a Disk, and none of that belongs on
// the request. The returned Upload carries nothing but a name, a size, an
// announced type and a way to open the bytes -- all of it the client's except
// the size.
//
// # The size limit is not here
//
// The body is read into memory or into a temporary file before this returns, so
// the thing that stops a four gigabyte upload is middleware.LimitBodySize on the
// way in, not a rule checked on the way out. The temporary file, when there is
// one, is removed when the request ends -- also when the request this Context
// carries is a copy a middleware made, which net/http's own cleanup does not
// reach -- so the bytes must be read during the request, which is what Disk.Put
// does.
func File(c *Context, field string) (filesystem.Upload, error) {
	if err := parseMultipart(c.Request); err != nil {
		return filesystem.Upload{}, fmt.Errorf("http: no file arrived in the %q field: %w", field, err)
	}
	headers := c.Request.MultipartForm.File[field]
	if len(headers) == 0 {
		return filesystem.Upload{}, fmt.Errorf("http: no file arrived in the %q field: %w", field, stdhttp.ErrMissingFile)
	}
	return filesystem.FromMultipart(field, headers[0]), nil
}
