package auth_test

import (
	"testing"

	"github.com/arandu-io/hesape/auth"
)

func TestARecallerIsValidOnlyWithAllOfItsSegments(t *testing.T) {
	cases := []struct {
		cookie string
		valid  bool
		why    string
	}{
		{"7|the-token|the-mac", true, "the three segments a recaller is made of"},
		{"7|the-token|the-mac|extra", true, "a fourth segment does not invalidate the first three"},
		{"7|the-token|", true, "the MAC may be empty: a cookie written before it existed still names its user"},
		{"7|the-token", false, "two segments: there is no MAC field at all"},
		{"7", false, "one segment, and not even a separator"},
		{"", false, "nothing"},
		{"|the-token|the-mac", false, "no user id"},
		{"   |the-token|the-mac", false, "a user id of spaces"},
		{"7||the-mac", false, "no remember token"},
		{"7|   |the-mac", false, "a remember token of spaces"},
	}

	for _, c := range cases {
		if got := auth.NewRecaller(c.cookie).Valid(); got != c.valid {
			t.Errorf("Valid(%q) = %v, want %v: %s", c.cookie, got, c.valid, c.why)
		}
	}
}

func TestARecallerIsTakenApartTheWayThePHPSplitsIt(t *testing.T) {
	recaller := auth.NewRecaller("7|the-token|the-mac")

	if recaller.ID() != "7" {
		t.Errorf("ID is %q", recaller.ID())
	}
	if recaller.Token() != "the-token" {
		t.Errorf("Token is %q", recaller.Token())
	}
	if recaller.Hash() != "the-mac" {
		t.Errorf("Hash is %q", recaller.Hash())
	}
	if segments := recaller.Segments(); len(segments) != 3 {
		t.Errorf("Segments has %d entries, want 3", len(segments))
	}
}

func TestARecallerWithAStrayPipeKeepsTheHashToItself(t *testing.T) {
	// The PHP splits into three for id and token, and into four for the hash.
	// So a token holding a pipe swallows the rest for Token, and Hash still
	// answers with the third field alone.
	recaller := auth.NewRecaller("7|the-token|the-mac|trailing")

	if recaller.ID() != "7" {
		t.Errorf("ID is %q", recaller.ID())
	}
	if recaller.Token() != "the-token" {
		t.Errorf("Token is %q", recaller.Token())
	}
	if recaller.Hash() != "the-mac" {
		t.Errorf("Hash is %q, want the third field alone", recaller.Hash())
	}
	if segments := recaller.Segments(); len(segments) != 4 {
		t.Errorf("Segments has %d entries, want 4", len(segments))
	}
}

func TestAMissingSegmentReadsAsEmptyRatherThanPanicking(t *testing.T) {
	recaller := auth.NewRecaller("7")

	if recaller.ID() != "7" {
		t.Errorf("ID is %q", recaller.ID())
	}
	if recaller.Token() != "" {
		t.Errorf("Token is %q, want empty", recaller.Token())
	}
	if recaller.Hash() != "" {
		t.Errorf("Hash is %q, want empty", recaller.Hash())
	}
}
