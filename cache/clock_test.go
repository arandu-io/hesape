package cache_test

import (
	"testing"

	"github.com/arandu-io/hesape/support"
)

// frozenClock pins support's clock where it is for the rest of the test and
// puts back whatever was pinned before when the test ends. The test then waits
// out a ttl with support.Travel, which is instant and exact: every store here
// reads the time through support.Now, so no test sleeps for an expiry.
func frozenClock(t *testing.T) {
	t.Helper()
	previous := support.GetTestNow()
	support.FreezeTime()
	t.Cleanup(func() { support.SetTestNow(previous) })
}
