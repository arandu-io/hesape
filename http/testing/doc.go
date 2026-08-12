// Package testing mirrors Illuminate\Http\Testing.
//
// The files it answers to, in the clone at
// laravel_illuminate/http/Testing:
//
//	File.php        -> file.go
//	FileFactory.php -> file.go
//	MimeType.php    -> mimetype.go
//
// # Where UploadedFile::fake lives, and why
//
// [Fake] is Illuminate\Http\UploadedFile::fake. It is here rather than on
// hesape/http.UploadedFile because Illuminate\Http\Testing\File extends
// UploadedFile: this package imports hesape/http, so a Fake on the parent would
// import this one back and close a cycle. Go has no class hierarchy to hang the
// static off, so the static lands beside what it builds.
//
// # The three MimeType statics keep their class prefix
//
// MimeType::from, MimeType::get and MimeType::search are [MimeTypeFrom],
// [MimeTypeGet] and [MimeTypeSearch]. Go has one namespace per package where
// PHP has one per class, and bare From, Get and Search in a package that also
// holds a file factory would say nothing about what they read.
package testing
