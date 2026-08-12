package testing

import (
	"mime"
	"path"
	"sort"
	"strings"
	"sync"
)

// DefaultMimeType is what MimeType::get answers when nothing is known about an
// extension: Symfony's Arr::first(...) ?? 'application/octet-stream'.
const DefaultMimeType = "application/octet-stream"

// mimeTypes answers to MimeType::$mime, the Symfony MimeTypes instance the PHP
// keeps in a static. Go has mime.TypeByExtension, which reads the same tables
// the operating system carries, plus the table below for the extensions that
// are not on every machine -- a test that passes on a laptop and fails on a
// build box because /etc/mime.types differs is the failure this closes.
var mimeTypes = struct {
	once  sync.Once
	extra map[string]string
}{
	extra: map[string]string{
		"bmp":  "image/bmp",
		"csv":  "text/csv",
		"doc":  "application/msword",
		"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"gif":  "image/gif",
		"htm":  "text/html",
		"html": "text/html",
		"jpeg": "image/jpeg",
		"jpg":  "image/jpeg",
		"js":   "text/javascript",
		"json": "application/json",
		"md":   "text/markdown",
		"mp3":  "audio/mpeg",
		"mp4":  "video/mp4",
		"pdf":  "application/pdf",
		"png":  "image/png",
		"svg":  "image/svg+xml",
		"txt":  "text/plain",
		"webp": "image/webp",
		"xls":  "application/vnd.ms-excel",
		"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"xml":  "application/xml",
		"zip":  "application/zip",
	},
}

// GetMimeTypes answers to MimeType::getMimeTypes: the table this package reads.
//
// The PHP returns a Symfony MimeTypes instance; Go's mime package is a set of
// functions rather than an object, so this returns the extension-to-type table
// [MimeTypeFrom] and [MimeTypeGet] consult -- which is what a caller of the PHP
// method reaches into it for.
func GetMimeTypes() map[string]string {
	mimeTypes.once.Do(func() {
		for extension, contentType := range mimeTypes.extra {
			// AddExtensionType is what makes mime.ExtensionsByType answer for
			// these too, which is what Search reads.
			_ = mime.AddExtensionType("."+extension, contentType)
		}
	})
	out := make(map[string]string, len(mimeTypes.extra))
	for extension, contentType := range mimeTypes.extra {
		out[extension] = contentType
	}
	return out
}

// MimeTypeFrom answers to MimeType::from: the type for a filename, read off its
// extension.
//
// It is MimeTypeFrom and not From because Go has one namespace per package and
// PHP has one per class: MimeType::from, MimeType::get and MimeType::search are
// three statics on a class whose whole purpose is the prefix, so the prefix is
// kept.
func MimeTypeFrom(filename string) string {
	return MimeTypeGet(strings.TrimPrefix(path.Ext(filename), "."))
}

// MimeTypeGet answers to MimeType::get: the type for an extension, or
// [DefaultMimeType].
func MimeTypeGet(extension string) string {
	table := GetMimeTypes()
	extension = strings.ToLower(strings.TrimPrefix(extension, "."))
	if extension == "" {
		return DefaultMimeType
	}
	if contentType, ok := table[extension]; ok {
		return contentType
	}
	if contentType := mime.TypeByExtension("." + extension); contentType != "" {
		if semi := strings.Index(contentType, ";"); semi >= 0 {
			contentType = strings.TrimSpace(contentType[:semi])
		}
		return contentType
	}
	return DefaultMimeType
}

// MimeTypeSearch answers to MimeType::search: an extension for a type, empty
// when none is known. The PHP returns string|null; empty is the null.
func MimeTypeSearch(contentType string) string {
	table := GetMimeTypes()
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if semi := strings.Index(contentType, ";"); semi >= 0 {
		contentType = strings.TrimSpace(contentType[:semi])
	}
	// Sorted, because two extensions share a type -- jpg and jpeg, htm and
	// html -- and a map range would answer a different one each run, which is
	// a test that fails one time in two.
	extensionsKnown := make([]string, 0, len(table))
	for extension := range table {
		extensionsKnown = append(extensionsKnown, extension)
	}
	sort.Strings(extensionsKnown)
	for _, extension := range extensionsKnown {
		if table[extension] == contentType {
			return extension
		}
	}
	extensions, err := mime.ExtensionsByType(contentType)
	if err != nil || len(extensions) == 0 {
		return ""
	}
	return strings.TrimPrefix(extensions[0], ".")
}
