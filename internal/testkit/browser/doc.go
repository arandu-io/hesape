// Package browser drives a page in a real Chrome the way the person using it
// does, and fails the test on what that person would have seen go wrong: an
// uncaught exception, a console.error, a response of 500 or more and a
// Content-Security-Policy violation.
//
// It exists only for the end-to-end tests of this module, which prove what
// the components do in a browser, and nothing outside the module can import
// it. The kit an application tests with lives in the framework module, above
// this one; this module cannot import it, so what its own tests need is kept
// here, and only that.
//
// Chrome is started with its debugging pipe, from CHROME_PATH or the first
// Chrome or Chromium found in the usual places. The tests that use it carry
// the e2e build tag, so the ordinary go test never starts a browser.
package browser
