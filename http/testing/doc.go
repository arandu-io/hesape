// Package testing helps a test build an UploadedFile without a real
// browser: a fake file of a given size or with given content, a fake
// image, and the MIME type lookups uploads are validated against.
//
// # Where Fake lives, and why
//
// [Fake] is here rather than on hesape/http.UploadedFile because
// testing.File embeds UploadedFile: this package imports hesape/http, so a
// Fake on the parent would import this one back and close a cycle. Go has
// no class hierarchy to hang a constructor off the child type, so it lands
// beside what it builds instead.
package testing
