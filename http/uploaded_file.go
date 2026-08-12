package http

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"strings"

	// The three decoders are imported for their side effect: image.DecodeConfig
	// answers Dimensions, and it only knows the formats that registered
	// themselves. PNG, JPEG and GIF are what getimagesize reads too.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/filesystem"
)

// ErrFileNotFound answers to
// Illuminate\Contracts\Filesystem\FileNotFoundException, which
// UploadedFile::get throws when the file behind the upload is gone.
var ErrFileNotFound = errors.New("http: the uploaded file is not readable")

// BaseFile mirrors Illuminate\Http\File: a file on disk, carrying the
// FileHelpers methods and nothing else.
//
// It is BaseFile and not File because [File] is already the function that pulls
// an upload off a Context, and a package cannot have both. The PHP name is on
// the doc line, which is where ADR 0044 says to put a mechanical change: Go has
// one namespace per package, and PHP has one per class.
type BaseFile struct {
	// pathname is the location on disk.
	pathname string
	// hashName caches FileHelpers::$hashName, so two calls to HashName on one
	// file answer the same name -- which is the whole reason the PHP caches it.
	hashName string
}

// NewBaseFile answers to File::__construct: a file at a path.
func NewBaseFile(pathname string) *BaseFile { return &BaseFile{pathname: pathname} }

// Path answers to FileHelpers::path: the fully-qualified path to the file.
func (f *BaseFile) Path() string { return f.pathname }

// GetRealPath answers to Symfony's File::getRealPath, which FileHelpers::path
// calls. It resolves symlinks; a path that cannot be resolved is answered as it
// stands, because the caller wanted a path and not an error about one.
func (f *BaseFile) GetRealPath() string {
	resolved, err := filepath.EvalSymlinks(f.pathname)
	if err != nil {
		return f.pathname
	}
	return resolved
}

// GetPathname answers to Symfony's File::getPathname.
func (f *BaseFile) GetPathname() string { return f.pathname }

// Extension answers to FileHelpers::extension: the extension guessed from the
// file's own type, without the dot, empty when nothing is known.
func (f *BaseFile) Extension() string { return f.GuessExtension() }

// GuessExtension answers to Symfony's File::guessExtension, which
// FileHelpers::extension calls. It answers from the name, because Go has no
// libmagic and the name is what a person sees.
func (f *BaseFile) GuessExtension() string {
	return strings.TrimPrefix(strings.ToLower(path.Ext(f.pathname)), ".")
}

// HashName answers to FileHelpers::hashName: a random 40-character name with
// the file's extension, under an optional directory.
//
// The name is drawn once per file and cached, so a caller that asks twice
// stores and links the same thing. The variadic argument is the PHP's optional
// $path.
func (f *BaseFile) HashName(dir ...string) string {
	prefix := ""
	if len(dir) > 0 && dir[0] != "" {
		prefix = strings.TrimRight(dir[0], "/") + "/"
	}
	if f.hashName == "" {
		f.hashName = strRandom(40)
	}
	extension := f.GuessExtension()
	if extension != "" {
		extension = "." + extension
	}
	return prefix + f.hashName + extension
}

// Dimensions answers to FileHelpers::dimensions: the width and height of the
// image, and whether it is one.
//
// The PHP returns array|null from getimagesize; this returns (width, height,
// ok), because a two-element array is a pair in Go and the null is the bool.
func (f *BaseFile) Dimensions() (int, int, bool) {
	handle, err := os.Open(f.GetRealPath())
	if err != nil {
		return 0, 0, false
	}
	defer handle.Close()

	config, _, err := image.DecodeConfig(handle)
	if err != nil {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

// GetSize answers to Symfony's File::getSize.
func (f *BaseFile) GetSize() int64 {
	info, err := os.Stat(f.pathname)
	if err != nil {
		return 0
	}
	return info.Size()
}

// GetMimeType answers to Symfony's File::getMimeType, guessed from the
// extension.
func (f *BaseFile) GetMimeType() string {
	return mime.TypeByExtension(strings.ToLower(path.Ext(f.pathname)))
}

// UploadedFile mirrors Illuminate\Http\UploadedFile: one file that arrived in a
// form field, with the FileHelpers methods and the four store variants.
//
// It embeds BaseFile because the PHP UploadedFile and File both `use
// FileHelpers`; the methods are the same methods on the same fields.
//
// # Storing one needs a Grant (RULE 14)
//
// The PHP reaches into the container for a filesystem and writes. Here the
// store methods take a context and an auth.Grant, because
// [filesystem.Disk.PutFileAs] does: a file is written under a tenant, the
// tenant comes off the Grant a policy minted, and there is no path from a form
// field to a storage prefix.
type UploadedFile struct {
	BaseFile

	// header is the multipart part this was read from, which is what Open and
	// the client-announced values come off.
	header *multipart.FileHeader
	// originalName is Symfony's $originalName: the name the client announced,
	// with any directory stripped.
	originalName string
	// mimeType is Symfony's $mimeType: the type the client announced. It is
	// recorded and never trusted.
	mimeType string
	// err is Symfony's $error: the upload error code, 0 when there is none.
	err int
	// test is Symfony's $test: whether this file was made by a factory rather
	// than by a browser, which is what makes IsValid true without a real
	// upload behind it.
	test bool
}

// NewUploadedFile answers to UploadedFile::__construct, and to
// UploadedFile::createFromBase.
//
// createFromBase promotes a Symfony UploadedFile into an Illuminate one. Go has
// no class inheritance and therefore no base instance to promote, and a second
// package-level CreateFromBase would collide with Request's -- PHP namespaces
// by class, Go by package. This is the constructor both PHP entry points reach.
func NewUploadedFile(header *multipart.FileHeader, field string) *UploadedFile {
	name := ""
	announced := ""
	if header != nil {
		name = path.Base(strings.ReplaceAll(header.Filename, `\`, "/"))
		announced = header.Header.Get("Content-Type")
	}
	return &UploadedFile{
		BaseFile:     BaseFile{pathname: name},
		header:       header,
		originalName: name,
		mimeType:     announced,
	}
}

// NewUploadedFileFromPath answers to UploadedFile::__construct in the shape the
// testing factory uses: a file that is already on disk, standing in for one
// that arrived. The test flag is Symfony's $test.
func NewUploadedFileFromPath(pathname, originalName, mimeType string, test bool) *UploadedFile {
	return &UploadedFile{
		BaseFile:     BaseFile{pathname: pathname},
		originalName: originalName,
		mimeType:     mimeType,
		test:         test,
	}
}

// GetClientOriginalName answers to Symfony's
// UploadedFile::getClientOriginalName: the name the client announced.
func (f *UploadedFile) GetClientOriginalName() string { return f.originalName }

// GetClientOriginalExtension answers to Symfony's
// UploadedFile::getClientOriginalExtension.
func (f *UploadedFile) GetClientOriginalExtension() string {
	return strings.TrimPrefix(strings.ToLower(path.Ext(f.originalName)), ".")
}

// GetClientMimeType answers to Symfony's UploadedFile::getClientMimeType: the
// type the client announced, which is a header somebody wrote.
func (f *UploadedFile) GetClientMimeType() string { return f.mimeType }

// ClientExtension answers to UploadedFile::clientExtension: the extension
// guessed from the type the client announced.
func (f *UploadedFile) ClientExtension() string { return f.GuessClientExtension() }

// GuessClientExtension answers to Symfony's
// UploadedFile::guessClientExtension, which clientExtension calls.
func (f *UploadedFile) GuessClientExtension() string {
	extensions, err := mime.ExtensionsByType(f.mimeType)
	if err != nil || len(extensions) == 0 {
		return f.GetClientOriginalExtension()
	}
	return strings.TrimPrefix(extensions[0], ".")
}

// GetError answers to Symfony's UploadedFile::getError.
func (f *UploadedFile) GetError() int { return f.err }

// IsValid answers to Symfony's UploadedFile::isValid: whether the upload
// completed and there are bytes behind it.
func (f *UploadedFile) IsValid() bool {
	if f.err != 0 {
		return false
	}
	if f.test {
		_, err := os.Stat(f.pathname)
		return err == nil
	}
	return f.header != nil && f.header.Size > 0
}

// GetSize answers to Symfony's File::getSize for an upload: the byte count the
// server counted as the body arrived, which is the one value on an upload that
// did not come from the client.
func (f *UploadedFile) GetSize() int64 {
	if f.header != nil {
		return f.header.Size
	}
	return f.BaseFile.GetSize()
}

// GuessExtension answers to Symfony's File::guessExtension for an upload: from
// the announced name, since the pathname of an upload is that name.
func (f *UploadedFile) GuessExtension() string {
	return strings.TrimPrefix(strings.ToLower(path.Ext(f.originalName)), ".")
}

// HashName answers to FileHelpers::hashName for an upload. It is redeclared so
// that the extension comes from the upload's own GuessExtension rather than the
// embedded BaseFile's.
func (f *UploadedFile) HashName(dir ...string) string {
	prefix := ""
	if len(dir) > 0 && dir[0] != "" {
		prefix = strings.TrimRight(dir[0], "/") + "/"
	}
	if f.hashName == "" {
		f.hashName = strRandom(40)
	}
	extension := f.GuessExtension()
	if extension != "" {
		extension = "." + extension
	}
	return prefix + f.hashName + extension
}

// Extension answers to FileHelpers::extension for an upload.
func (f *UploadedFile) Extension() string { return f.GuessExtension() }

// Open returns a reader over the bytes. It is what Get and the store methods
// read through, and it may be called more than once.
func (f *UploadedFile) Open() (io.ReadCloser, error) {
	if f.header != nil {
		handle, err := f.header.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrFileNotFound, err)
		}
		return handle, nil
	}
	handle, err := os.Open(f.pathname)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFileNotFound, err)
	}
	return handle, nil
}

// Get answers to UploadedFile::get: the contents of the uploaded file.
//
// The PHP throws FileNotFoundException when the file is not valid, so this
// returns ([]byte, error) and the error unwraps to [ErrFileNotFound].
func (f *UploadedFile) Get() ([]byte, error) {
	if !f.IsValid() {
		return nil, fmt.Errorf("%w: %s", ErrFileNotFound, f.pathname)
	}
	handle, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	return io.ReadAll(handle)
}

// Upload converts this file into the [filesystem.Upload] the storage layer
// takes. It is the one place the two vocabularies meet, so that the conversion
// is written once (RULE 9).
func (f *UploadedFile) Upload(field string) filesystem.Upload {
	if f.header != nil {
		return filesystem.FromMultipart(field, f.header)
	}
	return filesystem.Upload{
		Field:       field,
		Name:        f.originalName,
		Size:        f.GetSize(),
		ContentType: f.mimeType,
		Open:        func() (io.ReadCloser, error) { return f.Open() },
	}
}

// StoreOptions answers to the $options array UploadedFile::store and its three
// siblings take. The PHP accepts a string, which names the disk, or an array
// carrying a disk and a visibility; a struct says the same thing without a
// caller having to know the two key names.
type StoreOptions struct {
	// Disk is the disk to write to. The zero value is the default disk.
	Disk *filesystem.Disk
	// Visibility answers to the "visibility" key. StorePublicly and
	// StorePubliclyAs set it to "public".
	Visibility string
}

// Store answers to UploadedFile::store: write the file under a directory with a
// random name, and answer the key it landed on.
//
// The PHP returns string|false; this returns (string, error). The context and
// the Grant are what [filesystem.Disk.PutFileAs] requires, and requiring them
// here is RULE 14: a file is stored under the tenant a policy authorised, never
// under one read off the request.
func (f *UploadedFile) Store(ctx context.Context, g auth.Grant, directory string, options StoreOptions) (string, error) {
	return f.StoreAs(ctx, g, directory, f.HashName(), options)
}

// StorePublicly answers to UploadedFile::storePublicly: Store with the
// visibility set to public.
func (f *UploadedFile) StorePublicly(ctx context.Context, g auth.Grant, directory string, options StoreOptions) (string, error) {
	options.Visibility = filesystem.VisibilityPublic
	return f.StoreAs(ctx, g, directory, f.HashName(), options)
}

// StorePubliclyAs answers to UploadedFile::storePubliclyAs: StoreAs with the
// visibility set to public.
func (f *UploadedFile) StorePubliclyAs(ctx context.Context, g auth.Grant, directory, name string, options StoreOptions) (string, error) {
	options.Visibility = filesystem.VisibilityPublic
	return f.StoreAs(ctx, g, directory, name, options)
}

// StoreAs answers to UploadedFile::storeAs: write the file under a directory
// with the name given, and answer the key it landed on.
//
// The PHP returns string|false; this returns (string, error).
func (f *UploadedFile) StoreAs(ctx context.Context, g auth.Grant, directory, name string, options StoreOptions) (string, error) {
	if options.Disk == nil {
		return "", errors.New("http: storing an upload needs a disk: set StoreOptions.Disk to the filesystem.Disk it belongs on")
	}
	if !f.IsValid() {
		return "", fmt.Errorf("%w: %s", ErrFileNotFound, f.pathname)
	}
	return options.Disk.PutFileAs(ctx, g, directory, f.Upload(f.originalName), name)
}
