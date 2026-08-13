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
// # The three MimeType statics keep their names
//
// MimeType::from, MimeType::get and MimeType::search are [From], [Get] and
// [Search]. They read bare in a package that also holds a file factory, and
// that is the cost of ADR 0044: the name is the one the Laravel developer
// types, and a prefix invented to read better in Go is a method that, to them,
// is not there. Nothing else in this package claims those three names.
package testing
