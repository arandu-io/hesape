// This file is in the package rather than in session_test because the boundary
// it pins cannot be reached through a handler: the difference between `>` and
// `>=` is one instant on a nanosecond clock, and no test that reads the wall
// clock ever lands on it. The predicate is where the boundary is written, so
// the predicate is what is asserted on.
package session

import (
	"testing"
	"time"
)

// TestTheSessionThatIsExactlyOneLifetimeOldIsKept: under a two-hour
// lifetime, a session last written exactly two hours ago used to come back
// empty -- one request of being signed out early, on a tick nobody
// reproduces on purpose and nobody believes the report of.
func TestTheSessionThatIsExactlyOneLifetimeOldIsKept(t *testing.T) {
	lifetime := 2 * time.Hour

	if sessionIsExpired(lifetime, lifetime) {
		t.Error("a session exactly one lifetime old was dropped, and the boundary is inclusive")
	}
	if !sessionIsExpired(lifetime+time.Nanosecond, lifetime) {
		t.Error("a session past the lifetime was kept")
	}
	if sessionIsExpired(lifetime-time.Nanosecond, lifetime) {
		t.Error("a session inside the lifetime was dropped")
	}
	if sessionIsExpired(0, 0) {
		t.Error("a zero lifetime dropped a session written this instant")
	}
}
