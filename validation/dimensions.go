package validation

import (
	"io"
	"os"

	"github.com/arandu-io/hesape/internal/filetype"
)

type contentOpener interface {
	Open() (io.ReadCloser, error)
}

// imageDimensions measures an image, which is what `dimensions` compares.
//
// A trusted server-side Dimensioner is used first; UploadedFile implements it
// with its cached content inspection. Other files that can reopen their content
// are validated and decoded through the same bounded path as `image`.
func imageDimensions(f File) (width, height int, ok bool) {
	if d, isDimensioner := f.(Dimensioner); isDimensioner {
		return d.Dimensions()
	}
	if opener, opensContent := f.(contentOpener); opensContent {
		return filetype.Image(opener.Open, false)
	}
	path := f.GetRealPath()
	if path == "" {
		return 0, 0, false
	}
	return filetype.Image(func() (io.ReadCloser, error) { return os.Open(path) }, false)
}

func validImage(f File, allowSVG bool) bool {
	if d, isDimensioner := f.(Dimensioner); isDimensioner {
		_, _, valid := d.Dimensions()
		if valid {
			return true
		}
		return allowSVG && (f.GetMimeType() == "image/svg+xml" || f.GetMimeType() == "image/svg")
	}
	if opener, opensContent := f.(contentOpener); opensContent {
		_, _, ok := filetype.Image(opener.Open, allowSVG)
		return ok
	}
	if f.GetMimeType() == "image/svg+xml" || f.GetMimeType() == "image/svg" {
		return false
	}
	_, _, ok := imageDimensions(f)
	return ok
}
