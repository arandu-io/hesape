package support_test

import (
	"testing"

	"github.com/arandu-io/hesape/support"
)

// TestTheDeprecatedPlatformCheckReadsOnlyTheExactMarker: the deprecation tells
// a caller to replace the call with os.Getenv(...) == "1", so until it is
// removed the function must answer exactly that comparison -- true for "1" and
// for nothing else, not for "true" and not for an unset variable.
func TestTheDeprecatedPlatformCheckReadsOnlyTheExactMarker(t *testing.T) {
	for value, want := range map[string]bool{"1": true, "": false, "0": false, "true": false} {
		t.Setenv("LARAVEL_CLOUD", value)
		if got := support.Laravel_cloud(); got != want {
			t.Errorf("LARAVEL_CLOUD=%q: got %v, want %v", value, got, want)
		}
	}
}
