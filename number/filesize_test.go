package number

import (
	"math"
	"testing"
)

func TestFileSize(t *testing.T) {
	cases := []struct {
		bytes     int64
		precision int
		want      string
	}{
		{0, 0, "0 B"},
		{1, 0, "1 B"},
		{512, 1, "512.0 B"},
		{999, 0, "999 B"},
		{1000, 0, "1 KB"},
		{1536, 2, "1.54 KB"},
		{1000000, 0, "1 MB"},
		{1500000000, 1, "1.5 GB"},
		{-1000, 0, "-1 KB"},
		{math.MaxInt64, 2, "9.22 EB"},
	}
	for _, c := range cases {
		if got := FileSize(c.bytes, c.precision); got != c.want {
			t.Errorf("FileSize(%d, %d) = %q, want %q", c.bytes, c.precision, got, c.want)
		}
	}
}

func TestFileSizeBinary(t *testing.T) {
	cases := []struct {
		bytes     int64
		precision int
		want      string
	}{
		{0, 0, "0 B"},
		{1023, 0, "1,023 B"},
		{1024, 0, "1 KiB"},
		{1536, 1, "1.5 KiB"},
		{1 << 20, 0, "1 MiB"},
		{1 << 30, 0, "1 GiB"},
		{-1024, 0, "-1 KiB"},
		{math.MaxInt64, 2, "8.00 EiB"},
	}
	for _, c := range cases {
		if got := FileSizeBinary(c.bytes, c.precision); got != c.want {
			t.Errorf("FileSizeBinary(%d, %d) = %q, want %q", c.bytes, c.precision, got, c.want)
		}
	}
}

// TestFileSizeStopsAtTheLastUnit guards the loop bound: a byte count large
// enough to run past the table has to stay on the last unit rather than index
// out of it.
func TestFileSizeStopsAtTheLastUnit(t *testing.T) {
	if got := FileSize(math.MinInt64, 0); got != "-9 EB" {
		t.Errorf("FileSize(MinInt64, 0) = %q, want %q", got, "-9 EB")
	}
}
