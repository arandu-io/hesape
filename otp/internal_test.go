package otp

import (
	"encoding/hex"
	"testing"
)

// TestTruncateMatchesTheRFC4226IntermediateValues pins the dynamic truncation
// on its own, against the digests and truncated numbers RFC 4226 publishes in
// Appendix D.
//
// HOTP is proved by its own test in the package's public tests, and it would
// catch a broken offset too. This test exists because it says which half broke:
// a wrong offset here is arithmetic, and a wrong code there with a right number
// here is the decimal reduction or the padding.
func TestTruncateMatchesTheRFC4226IntermediateValues(t *testing.T) {
	// Appendix D, Table 1 and Table 2: the HMAC-SHA-1 of each counter under the
	// ASCII secret "12345678901234567890", the four bytes dynamic truncation
	// selects, and the number they make.
	cases := []struct {
		counter   uint64
		digest    string
		truncated string
		decimal   uint32
	}{
		{0, "cc93cf18508d94934c64b65d8ba7667fb7cde4b0", "4c93cf18", 1284755224},
		{1, "75a48a19d4cbe100644e8ac1397eea747a2d33ab", "41397eea", 1094287082},
		{2, "0bacb7fa082fef30782211938bc1c5e70416ff44", "082fef30", 137359152},
		{3, "66c28227d03a2d5529262ff016a1e6ef76557ece", "66ef7655", 1726969429},
		{4, "a904c900a64b35909874b33e61c5938a8e15ed1c", "61c5938a", 1640338314},
		{5, "a37e783d7b7233c083d4f62926c7a25f238d0316", "33c083d4", 868254676},
		{6, "bc9cd28561042c83f219324d3c607256c03272ae", "7256c032", 1918287922},
		{7, "a4fb960c0bc06e1eabb804e5b397cdc4b45596fa", "04e5b397", 82162583},
		{8, "1b3c89f65e6c9e883012052823443f048b4332db", "2823443f", 673399871},
		{9, "1637409809a679dc698207310c8c7fc07290d9e5", "2679dc69", 645520489},
	}

	for _, c := range cases {
		digest, err := hex.DecodeString(c.digest)
		if err != nil {
			t.Errorf("counter %d: the fixture digest is not hexadecimal: %v", c.counter, err)
			continue
		}
		truncated, err := hex.DecodeString(c.truncated)
		if err != nil {
			t.Errorf("counter %d: the fixture truncation is not hexadecimal: %v", c.counter, err)
			continue
		}

		got := truncate(digest)
		if got != c.decimal {
			t.Errorf("counter %d: truncate returned %d, and the RFC says %d", c.counter, got, c.decimal)
		}

		// The same assertion read the other way round: the bytes the RFC names
		// are the bytes the offset selects.
		offset := int(digest[len(digest)-1] & 0x0f)
		selected := digest[offset : offset+4]
		selected[0] &= 0x7f
		if hex.EncodeToString(selected) != c.truncated {
			t.Errorf("counter %d: the offset selected %s, and the RFC says %s",
				c.counter, hex.EncodeToString(selected), hex.EncodeToString(truncated))
		}
	}
}

// TestResolveOnlyFillsInTheZeroValue fixes the one piece of magic in [TOTP]: a
// configuration with nothing set means the defaults, and a configuration with
// anything set is read exactly as written.
func TestResolveOnlyFillsInTheZeroValue(t *testing.T) {
	if got := (TOTP{}).Resolve(); got != Default() {
		t.Errorf("the zero value resolved to %+v, and it should be %+v", got, Default())
	}

	partial := TOTP{Digits: MaxDigits}
	if got := partial.Resolve(); got != partial {
		t.Errorf("a partly filled configuration resolved to %+v, and it should have been left alone", got)
	}
	if err := partial.Validate(); err == nil {
		t.Error("a configuration with digits and no period validated, and it cannot produce a code")
	}
}
