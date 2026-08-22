package number_test

import (
	"math"
	"testing"

	"github.com/arandu-io/hesape/number"
)

func TestFileSize(t *testing.T) {
	cases := []struct {
		bytes     float64
		precision int
		want      string
	}{
		{0, 0, "0 B"},
		{1, 0, "1 B"},
		{512, 1, "512.0 B"},
		{900, 0, "900 B"},
		{1000, 0, "1 KB"},
		{1536, 2, "1.54 KB"},
		{1000000, 0, "1 MB"},
		{1500000000, 1, "1.5 GB"},
		{math.MaxInt64, 2, "9.22 EB"},
	}
	for _, c := range cases {
		if got := number.FileSize(c.bytes, c.precision, -1, false); got != c.want {
			t.Errorf("FileSize(%v, %d, -1, false) = %q, want %q", c.bytes, c.precision, got, c.want)
		}
	}
}

// TestFileSizeStepsUpAtNineTenthsOfTheBase pins the bound: the unit changes
// when the count is more than nine tenths of the base, not when it reaches the
// base, so 999 bytes read as a kilobyte.
func TestFileSizeStepsUpAtNineTenthsOfTheBase(t *testing.T) {
	if got := number.FileSize(999, 0, -1, false); got != "1 KB" {
		t.Errorf("FileSize(999, 0, -1, false) = %q, want %q", got, "1 KB")
	}
	if got := number.FileSize(901, 3, -1, false); got != "0.901 KB" {
		t.Errorf("FileSize(901, 3, -1, false) = %q, want %q", got, "0.901 KB")
	}
}

func TestFileSizeBinaryPrefix(t *testing.T) {
	cases := []struct {
		bytes     float64
		precision int
		want      string
	}{
		{0, 0, "0 B"},
		{921, 0, "921 B"},
		{1024, 0, "1 KiB"},
		{1536, 1, "1.5 KiB"},
		{1 << 20, 0, "1 MiB"},
		{1 << 30, 0, "1 GiB"},
		{math.MaxInt64, 2, "8.00 EiB"},
	}
	for _, c := range cases {
		if got := number.FileSize(c.bytes, c.precision, -1, true); got != c.want {
			t.Errorf("FileSize(%v, %d, -1, true) = %q, want %q", c.bytes, c.precision, got, c.want)
		}
	}
}

// TestFileSizeLeavesANegativeCountAlone records the shape of the loop: its
// bound is a plain greater-than, which no negative count passes, so the value
// keeps the byte unit instead of being scaled.
func TestFileSizeLeavesANegativeCountAlone(t *testing.T) {
	if got := number.FileSize(-1000, 0, -1, false); got != "-1,000 B" {
		t.Errorf("FileSize(-1000, 0, -1, false) = %q, want %q", got, "-1,000 B")
	}
}

// TestFileSizeMaxPrecisionDropsTrailingZeros separates the two digit counts:
// a precision keeps the zeros it asked for and a maximum precision does not.
func TestFileSizeMaxPrecisionDropsTrailingZeros(t *testing.T) {
	if got := number.FileSize(1500, 0, 3, false); got != "1.5 KB" {
		t.Errorf("FileSize(1500, 0, 3, false) = %q, want %q", got, "1.5 KB")
	}
	if got := number.FileSize(1500, 3, -1, false); got != "1.500 KB" {
		t.Errorf("FileSize(1500, 3, -1, false) = %q, want %q", got, "1.500 KB")
	}
}
