//go:build e2e

package foundation

import "net/http"

// LiveReloadForTest is the development middleware, for the browser test that
// lives in the external test package: that package imports view, which imports
// this one, so it cannot be written here.
func LiveReloadForTest(next http.Handler) http.Handler { return liveReload(next) }

// SetReloadTagForTest fills the tag Boot would take from the view module, and
// returns what puts it back.
func SetReloadTagForTest(tag string) func() {
	reloadTag = []byte(tag)
	return func() { reloadTag = nil }
}
