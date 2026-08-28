package encryption

import "testing"

// TestNothingCanMoveTheBoundaryBetweenThePurposeAndTheBody holds the length
// prefix in sign, from inside the package because it cannot be reached from
// outside it.
//
// Verify refuses a forged token with or without the prefix, which is what makes
// this test necessary rather than redundant. The body of a token is base64url
// text and a digit string, and neither alphabet holds the "|" that a moved
// boundary needs, so a forgery that gets past the MAC dies one line later on an
// unreadable payload. The prefix could therefore be deleted with every other
// test in this package still passing, and the loss would be a link signed for
// one purpose that is also signed for another.
//
// The shape is not hypothetical. A purpose naming a tenant carries a separator,
// and every payload signed through here is built one field at a time behind its
// own length for this same reason.
func TestNothingCanMoveTheBoundaryBetweenThePurposeAndTheBody(t *testing.T) {
	// Any key holds the property; this one is 32 bytes so it is the length the
	// application actually signs with.
	s := NewSigner([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))

	for _, tt := range []struct{ name, longPurpose, shortPurpose, longBody, shortBody string }{
		// Run together across the separator, each pair spells one byte string:
		// "a|b|c" and "reset|tenant-1|eA.9999999999".
		{"one character either side", "a|b", "a", "c", "b|c"},
		{"a purpose naming a tenant", "reset|tenant-1", "reset", "eA.9999999999", "tenant-1|eA.9999999999"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if s.sign(tt.longPurpose, tt.longBody) == s.sign(tt.shortPurpose, tt.shortBody) {
				t.Fatalf("a token signed for %q is signed for %q as well: the length in front of the "+
					"purpose is gone, and one link is valid for two things", tt.longPurpose, tt.shortPurpose)
			}
		})
	}

	// The honest half. Without it this test would pass on a sign that returned
	// something different every call, which signs nothing at all.
	if s.sign("a|b", "c") != s.sign("a|b", "c") {
		t.Error("the same purpose and body signed to two different values")
	}
}
