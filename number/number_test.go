package number

import (
	"math"
	"testing"
)

func TestFormat(t *testing.T) {
	cases := []struct {
		value     float64
		precision int
		want      string
	}{
		{0, 0, "0"},
		{123, 0, "123"},
		{1000, 0, "1,000"},
		{1000000, 0, "1,000,000"},
		{1234.5678, 2, "1,234.57"},
		{1234.5678, 0, "1,235"},
		{-1234.5, 1, "-1,234.5"},
		{-1, 0, "-1"},
		{0.5, 3, "0.500"},
		{12345.6, -3, "12,346"},
		{-0.004, 2, "0.00"},
		{2.5, 0, "2"},
		{3.5, 0, "4"},
	}
	for _, c := range cases {
		if got := Format(c.value, c.precision); got != c.want {
			t.Errorf("Format(%v, %d) = %q, want %q", c.value, c.precision, got, c.want)
		}
	}
}

func TestFormatSpecialValues(t *testing.T) {
	if got := Format(math.NaN(), 2); got != "NaN" {
		t.Errorf("Format(NaN, 2) = %q, want %q", got, "NaN")
	}
	if got := Format(math.Inf(1), 2); got != "Inf" {
		t.Errorf("Format(+Inf, 2) = %q, want %q", got, "Inf")
	}
	if got := Format(math.Inf(-1), 2); got != "-Inf" {
		t.Errorf("Format(-Inf, 2) = %q, want %q", got, "-Inf")
	}
}

func TestPercentage(t *testing.T) {
	cases := []struct {
		value     float64
		precision int
		want      string
	}{
		{0, 0, "0%"},
		{12.5, 1, "12.5%"},
		{12.5, 0, "12%"},
		{100, 0, "100%"},
		{-7.25, 2, "-7.25%"},
		{1234.5, 1, "1,234.5%"},
	}
	for _, c := range cases {
		if got := Percentage(c.value, c.precision); got != c.want {
			t.Errorf("Percentage(%v, %d) = %q, want %q", c.value, c.precision, got, c.want)
		}
	}
	if got := Percentage(math.NaN(), 2); got != "NaN" {
		t.Errorf("Percentage(NaN, 2) = %q, want %q", got, "NaN")
	}
}

func TestOrdinal(t *testing.T) {
	cases := []struct {
		value int
		want  string
	}{
		{0, "0th"},
		{1, "1st"},
		{2, "2nd"},
		{3, "3rd"},
		{4, "4th"},
		{10, "10th"},
		{11, "11th"},
		{12, "12th"},
		{13, "13th"},
		{21, "21st"},
		{22, "22nd"},
		{23, "23rd"},
		{100, "100th"},
		{101, "101st"},
		{111, "111th"},
		{112, "112th"},
		{1000, "1,000th"},
		{1002, "1,002nd"},
		{-1, "-1st"},
		{-11, "-11th"},
	}
	for _, c := range cases {
		if got := Ordinal(c.value); got != c.want {
			t.Errorf("Ordinal(%d) = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestAbbreviate(t *testing.T) {
	cases := []struct {
		value     float64
		precision int
		want      string
	}{
		{0, 0, "0"},
		{0, 2, "0.00"},
		{999, 0, "999"},
		{1000, 0, "1K"},
		{1500, 1, "1.5K"},
		{-1500, 1, "-1.5K"},
		{1000000, 0, "1M"},
		{-2000000, 0, "-2M"},
		{1e9, 0, "1B"},
		{1.23e12, 2, "1.23T"},
		{1e15, 0, "1Q"},
		{1e18, 0, "1,000Q"},
		{999999, 0, "1,000K"},
	}
	for _, c := range cases {
		if got := Abbreviate(c.value, c.precision); got != c.want {
			t.Errorf("Abbreviate(%v, %d) = %q, want %q", c.value, c.precision, got, c.want)
		}
	}
	if got := Abbreviate(math.Inf(1), 0); got != "Inf" {
		t.Errorf("Abbreviate(+Inf, 0) = %q, want %q", got, "Inf")
	}
}
