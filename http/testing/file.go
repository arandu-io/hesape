package testing

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path"
	"strings"

	hhttp "github.com/arandu-io/hesape/http"
)

// File is an UploadedFile a test made, with a size and a type it reports
// rather than ones a browser sent.
//
// It embeds *hesape/http.UploadedFile, so everything an upload answers --
// HashName, Extension, Get, StoreAs -- is the same method on the same
// file.
type File struct {
	*hhttp.UploadedFile

	// Name is the name the fake announces.
	Name string
	// TempFile is the file on disk holding the bytes. It is removed by
	// [File.Close].
	TempFile *os.File
	// SizeToReport is the size [File.Size] set, in bytes.
	SizeToReport int64
	// MimeTypeToReport is the type [File.MimeType] set.
	MimeTypeToReport string
}

// NewFile builds a fake around a name and a temporary file already holding
// the bytes.
func NewFile(name string, tempFile *os.File) *File {
	f := &File{Name: name, TempFile: tempFile}
	f.MimeTypeToReport = ""
	f.UploadedFile = hhttp.NewUploadedFileFromPath(
		f.tempFilePath(), name, f.GetMimeType(), true,
	)
	return f
}

// tempFilePath is the path of the temporary file, or empty when there is
// none.
func (f *File) tempFilePath() string {
	if f.TempFile == nil {
		return ""
	}
	return f.TempFile.Name()
}

// Create is a fake file of the given size in kilobytes. For content
// instead of a size, use [CreateWithContent]. The variadic argument
// defaults to 0.
func Create(name string, kilobytes ...int) (*File, error) {
	return NewFileFactory().Create(name, kilobytes...)
}

// CreateWithContent is a fake file holding the given content.
func CreateWithContent(name, content string) (*File, error) {
	return NewFileFactory().CreateWithContent(name, content)
}

// Image is a fake image of the given size. The variadic arguments default
// to 10 and 10.
func Image(name string, size ...int) (*File, error) {
	return NewFileFactory().Image(name, size...)
}

// Size sets the size the fake reports, in kilobytes.
func (f *File) Size(kilobytes int) *File {
	f.SizeToReport = int64(kilobytes) * 1024
	return f
}

// GetSize is the size the fake reports, falling back to the size on disk.
func (f *File) GetSize() int64 {
	if f.SizeToReport > 0 {
		return f.SizeToReport
	}
	if f.UploadedFile == nil {
		return 0
	}
	return f.UploadedFile.GetSize()
}

// MimeType sets the type the fake reports.
func (f *File) MimeType(contentType string) *File {
	f.MimeTypeToReport = contentType
	return f
}

// GetMimeType is the type the fake reports, falling back to the one the
// name implies.
func (f *File) GetMimeType() string {
	if f.MimeTypeToReport != "" {
		return f.MimeTypeToReport
	}
	return From(f.Name)
}

// Close removes the temporary file. It is what a test defers, since
// nothing removes it automatically.
func (f *File) Close() error {
	if f.TempFile == nil {
		return nil
	}
	name := f.TempFile.Name()
	_ = f.TempFile.Close()
	return os.Remove(name)
}

// FileFactory is the three ways to make a fake upload.
type FileFactory struct{}

// NewFileFactory builds a FileFactory. See the package doc for why the
// entry point ([Fake]) is here and not on hesape/http.UploadedFile.
func NewFileFactory() *FileFactory { return &FileFactory{} }

// Fake begins creating a new file fake.
//
// It is in this package and not on hesape/http.UploadedFile because File
// embeds UploadedFile: this sub-package imports the parent, so a Fake on
// the parent would import the sub-package back and close a cycle.
func Fake() *FileFactory { return NewFileFactory() }

// Create is a fake file of the given size in kilobytes. The variadic
// argument defaults to 0.
//
// Returns (*File, error): making a temporary file can fail.
func (f *FileFactory) Create(name string, args ...int) (*File, error) {
	kilobytes := 0
	if len(args) > 0 {
		kilobytes = args[0]
	}
	temp, err := tempFile(name)
	if err != nil {
		return nil, err
	}
	file := NewFile(name, temp)
	file.SizeToReport = int64(kilobytes) * 1024
	return file, nil
}

// CreateWithMimeType is Create with the type the fake reports also set. It
// is a separate method because Go has no optional argument that can be
// skipped over.
func (f *FileFactory) CreateWithMimeType(name string, kilobytes int, contentType string) (*File, error) {
	file, err := f.Create(name, kilobytes)
	if err != nil {
		return nil, err
	}
	return file.MimeType(contentType), nil
}

// CreateWithContent is a fake file holding the given bytes, reporting
// their length as its size.
func (f *FileFactory) CreateWithContent(name, content string) (*File, error) {
	temp, err := tempFile(name)
	if err != nil {
		return nil, err
	}
	if _, err := temp.WriteString(content); err != nil {
		return nil, fmt.Errorf("http/testing: writing the fake file %q: %w", name, err)
	}
	if err := temp.Sync(); err != nil {
		return nil, fmt.Errorf("http/testing: writing the fake file %q: %w", name, err)
	}
	file := NewFile(name, temp)
	file.SizeToReport = int64(len(content))
	return file, nil
}

// Image is a fake image of the given size, encoded in the format the
// name's extension names.
//
// PNG, JPEG and GIF are what image/png, image/jpeg and image/gif encode;
// anything else falls back to JPEG.
//
// The variadic arguments are width and height, defaulting to 10 and 10.
func (f *FileFactory) Image(name string, size ...int) (*File, error) {
	width, height := 10, 10
	if len(size) > 0 {
		width = size[0]
	}
	if len(size) > 1 {
		height = size[1]
	}

	encoded, err := generateImage(width, height, strings.TrimPrefix(path.Ext(name), "."))
	if err != nil {
		return nil, err
	}
	temp, err := tempFile(name)
	if err != nil {
		return nil, err
	}
	if _, err := temp.Write(encoded); err != nil {
		return nil, fmt.Errorf("http/testing: writing the fake image %q: %w", name, err)
	}
	if err := temp.Sync(); err != nil {
		return nil, fmt.Errorf("http/testing: writing the fake image %q: %w", name, err)
	}
	file := NewFile(name, temp)
	file.SizeToReport = int64(len(encoded))
	return file, nil
}

// generateImage draws a solid black image of the given size and encodes it
// in the given format.
func generateImage(width, height int, extension string) ([]byte, error) {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			canvas.Set(x, y, color.Black)
		}
	}

	var buffer bytes.Buffer
	var err error
	switch strings.ToLower(extension) {
	case "png":
		err = png.Encode(&buffer, canvas)
	case "gif":
		err = gif.Encode(&buffer, canvas, nil)
	default:
		err = jpeg.Encode(&buffer, canvas, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("http/testing: encoding the fake image: %w", err)
	}
	return buffer.Bytes(), nil
}

// tempFile is a file in the temporary directory keeping the name's
// extension, so that the type the fake reports and the bytes on disk
// agree.
func tempFile(name string) (*os.File, error) {
	pattern := "hesape-fake-*" + path.Ext(name)
	handle, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, fmt.Errorf("http/testing: creating a temporary file for %q: %w", name, err)
	}
	return handle, nil
}
