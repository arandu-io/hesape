// Package promises mirrors Illuminate\Http\Client\Promises.
//
// It holds promise implementations for async HTTP requests.
//
//	LazyPromise.php
//	FluentPromise.php
//
// In Go, async requests use goroutines and channels natively;
// the package exists for parity with the Illuminate surface.
package promises
