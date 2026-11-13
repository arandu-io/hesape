package validation

import (
	"bytes"
	_ "embed"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// The two lists below are read once, on the first Verify. The first is the
// SecLists 10k-most-common list, unmodified (its notice is in THIRD_PARTY.md);
// the second is this project's own addition of what is common in Brazil.
var (
	//go:embed common_passwords.txt
	commonPasswordsList string

	//go:embed common_passwords_br.txt
	commonPasswordsBrazil string
)

// maxAnalysed is the longest input CommonPasswords reads in full, in runes. It
// keeps the cost of one Verify a number: the length rule of a policy refuses
// anything longer than a password field takes long before this runs, and a
// caller that reaches Verify with a megabyte of text gets the two linear checks
// that need no table instead of an analysis that grows with the input.
const maxAnalysed = 128

// minCoreRunes is the shortest piece of a password that counts as a common word
// once digits and symbols have been trimmed off its ends. Shorter than this, a
// word is too likely to be the first letters of a good password.
const minCoreRunes = 4

// CommonPasswords returns a verifier that refuses the passwords everybody
// tries first, with no network: it reads two lists embedded in the binary.
//
//	rule := validation.PasswordMin(12).Max(128).Uncompromised(validation.CommonPasswords())
//
// The lists are about ten thousand English passwords and a few hundred common
// in Brazil, and an exact match is not what makes it useful, because a minimum
// of twelve characters leaves no list entry that long. What is compared is the
// shape of the password, so these are refused as well:
//
//   - the same text in another case, or with a trailing or leading run of
//     digits and symbols (Password1234, Senha@2026!, qwerty123456);
//   - leetspeak and accents folded back to letters (p@ssw0rd, s3nh@);
//   - a common word repeated (passwordpassword), or any short unit repeated
//     (abababababab, 1231231231);
//   - one character repeated (aaaaaaaaaaaa);
//   - an ascending or descending run of digits or letters (123456789012,
//     abcdefghijkl, 987654321098), and a walk across the keyboard (qwertyuiopas,
//     1q2w3e4r5t6y, 1qaz2wsx3edc), alone or chained one after another.
//
// A passphrase of unrelated words, and a random password, are not refused:
// nothing is removed from inside the text, two different common words side by
// side are not a pattern, and a piece shorter than four characters is never
// looked up after a trim.
//
// The threshold Password.Uncompromised takes is ignored: a password is on the
// list or it is not, there is no count of how often it leaked. The verifier is
// stateless and safe for concurrent use, and a Verify allocates nothing for any
// password of up to 128 runes. A longer input is refused only when the whole of
// it is one repeated character or one sequential run. Text that is not valid
// UTF-8 is read one byte at a time as U+FFFD and never panics.
//
// A refusal says the password is too common, in the language of the validator,
// instead of saying it appeared in a leak.
func CommonPasswords() UncompromisedVerifier { return commonPasswordVerifier{} }

// commonPasswordVerifier is the value CommonPasswords returns. It holds
// nothing: the tables are package state, built once and read-only after.
type commonPasswordVerifier struct{}

var _ UncompromisedReason = commonPasswordVerifier{}

// Verify reports false when the value is a common password or a trivial
// variation of one. The threshold is not used.
func (commonPasswordVerifier) Verify(value string, _ int) bool { return !isCommonPassword(value) }

// Refusal is the sentence said when Verify reports false: the translation key
// and the English text to fall back on.
func (commonPasswordVerifier) Refusal() (key, fallback string) {
	return "validation.password.common",
		"The given :attribute is too common. Please choose a different :attribute."
}

// UncompromisedReason is implemented by an UncompromisedVerifier that refuses
// for a reason other than a data leak, so the message says so.
//
// Password.Uncompromised asks the verifier that refused for it. Refusal returns
// the key of the line in the translation files and the English sentence used
// when the translator has none, which is the pair the rule's own lines carry.
// A verifier that does not implement it is refused with the leak sentence.
type UncompromisedReason interface {
	UncompromisedVerifier

	// Refusal returns the translation key and the English fallback of the
	// sentence for a value Verify reported false for.
	Refusal() (key, fallback string)
}

// commonTable is the lists after normalisation, built on first use.
var commonTable struct {
	once  sync.Once
	words map[string]struct{}
	walks [][]byte
}

func commonTables() (map[string]struct{}, [][]byte) {
	commonTable.once.Do(func() {
		words := make(map[string]struct{}, 11000)
		for _, source := range []string{commonPasswordsList, commonPasswordsBrazil} {
			for len(source) > 0 {
				var line string
				line, source, _ = strings.Cut(source, "\n")
				line = strings.TrimRight(line, "\r")
				if line == "" {
					continue
				}
				words[skeleton(line)] = struct{}{}
			}
		}
		commonTable.words = words
		commonTable.walks = keyboardWalks()
	})
	return commonTable.words, commonTable.walks
}

// skeleton is a list word as it is compared: lower case, accents and leetspeak
// folded. The same fold is applied to what the user typed, so both sides agree
// whichever way an ambiguous character (1 as i or as l) was meant.
func skeleton(word string) string {
	var out strings.Builder
	for _, r := range word {
		out.WriteRune(leetRune(lowerRune(r)))
	}
	return out.String()
}

// lowerRune is the rune in lower case with the accents of Portuguese, Spanish
// and French folded to the plain letter.
func lowerRune(r rune) rune {
	r = unicode.ToLower(r)
	if r < 0xC0 || r > 0xFF {
		return r
	}
	switch r {
	case 'à', 'á', 'â', 'ã', 'ä', 'å':
		return 'a'
	case 'ç':
		return 'c'
	case 'è', 'é', 'ê', 'ë':
		return 'e'
	case 'ì', 'í', 'î', 'ï':
		return 'i'
	case 'ñ':
		return 'n'
	case 'ò', 'ó', 'ô', 'õ', 'ö':
		return 'o'
	case 'ù', 'ú', 'û', 'ü':
		return 'u'
	case 'ý', 'ÿ':
		return 'y'
	}
	return r
}

// leetRune folds the usual substitutions of a digit or a symbol for a letter.
// l folds to i with 1 and !, so that the three are one character on both sides
// of the comparison.
func leetRune(r rune) rune {
	switch r {
	case '@', '4':
		return 'a'
	case '8':
		return 'b'
	case '3':
		return 'e'
	case '6':
		return 'g'
	case '1', '!', '|', 'l':
		return 'i'
	case '0':
		return 'o'
	case '$', '5':
		return 's'
	case '7', '+':
		return 't'
	}
	return r
}

// shape is one password read as runes: low is lower case with accents folded,
// fold is low with leetspeak folded too. They stay on the stack.
type shape struct {
	low  [maxAnalysed]rune
	fold [maxAnalysed]rune
}

func isCommonPassword(value string) bool {
	if value == "" {
		return false
	}
	n := utf8.RuneCountInString(value)
	if n > maxAnalysed {
		return longTrivial(value)
	}

	var s shape
	i := 0
	for _, r := range value {
		s.low[i] = lowerRune(r)
		s.fold[i] = leetRune(s.low[i])
		i++
	}
	low, fold := s.low[:n], s.fold[:n]

	if weak(low, fold, false) {
		return true
	}

	// The letters in the middle, with whatever digits and symbols were put
	// before and after them left off. The whole text was asked about above.
	start, end := letterSpan(low)
	if end > start && (start > 0 || end < n) && weak(low[start:end], fold[start:end], false) {
		return true
	}
	return false
}

// letterSpan is the range of low from its first letter to its last.
func letterSpan(low []rune) (start, end int) {
	start = len(low)
	for i, r := range low {
		if unicode.IsLetter(r) {
			start = i
			break
		}
	}
	for i := len(low) - 1; i >= start && i >= 0; i-- {
		if unicode.IsLetter(low[i]) {
			end = i + 1
			break
		}
	}
	if end < start {
		return 0, 0
	}
	return start, end
}

// weak reports whether the text is, as a whole, a listed word with any run of
// digits and symbols cut off either end, a pattern, or a short unit repeated.
// A unit that was itself found by repetition is not asked about repetition
// again.
func weak(low, fold []rune, unit bool) bool {
	if len(low) == 0 {
		return false
	}
	if patterned(low) || listed(low, fold) {
		return true
	}
	if unit {
		return false
	}
	p := period(low)
	if p == 0 || p == len(low) {
		return false
	}
	if p < minCoreRunes && len(low) >= minCoreRunes {
		return true
	}
	return weak(low[:p], fold[:p], true)
}

// listed looks the folded text up as it is and with every cut of the digits and
// symbols at its ends: each position inside the run at the end, and the run at
// the start dropped whole. The whole text matches at any length; a cut one only
// when what is left is at least minCoreRunes long.
func listed(low, fold []rune) bool {
	words, _ := commonTables()
	n := len(low)
	first, last := letterSpan(low)

	var buf [64]byte
	look := func(from, to int) bool {
		key := buf[:0]
		for _, r := range fold[from:to] {
			key = utf8.AppendRune(key, r)
		}
		_, ok := words[string(key)]
		return ok
	}

	if look(0, n) {
		return true
	}
	if last <= first {
		return false
	}
	for end := last; end <= n; end++ {
		if end-first >= minCoreRunes && look(first, end) {
			return true
		}
		if first > 0 && end-0 >= minCoreRunes && end != n && look(0, end) {
			return true
		}
	}
	return false
}

// period is the length of the shortest unit that, repeated, spells the whole
// text, or the length of the text when there is none.
func period(r []rune) int {
	n := len(r)
	for p := 1; p <= n/2; p++ {
		if n%p != 0 {
			continue
		}
		repeats := true
		for i := p; i < n; i++ {
			if r[i] != r[i-p] {
				repeats = false
				break
			}
		}
		if repeats {
			return p
		}
	}
	return n
}

// patterned reports whether the text is one pattern or a chain of them, each
// four runes or more: a repeated character, a sequential run, or a walk across
// the keyboard.
//
// Every prefix of such a pattern is a pattern too, so for a start the scan
// stops at the first length that fails, and a password with no pattern in it
// costs one check per position.
func patterned(r []rune) bool {
	n := len(r)
	if n < 4 {
		return false
	}
	var reach [maxAnalysed + 1]bool
	reach[0] = true
	for i := 0; i < n; i++ {
		if !reach[i] {
			continue
		}
		for j := i + 4; j <= n; j++ {
			if !isPattern(r[i:j]) {
				break
			}
			reach[j] = true
		}
	}
	return reach[n]
}

func isPattern(r []rune) bool {
	return uniform(r) || sequential(r) || keyboardWalk(r)
}

// uniform is one rune repeated.
func uniform(r []rune) bool {
	if len(r) < 2 {
		return false
	}
	for _, c := range r[1:] {
		if c != r[0] {
			return false
		}
	}
	return true
}

// sequential is a run of consecutive digits or letters, going up or going
// down. Both alphabets wrap, so 890123 and xyzabc count.
func sequential(r []rune) bool {
	if len(r) < 2 {
		return false
	}
	var base, size rune
	switch {
	case r[0] >= '0' && r[0] <= '9':
		base, size = '0', 10
	case r[0] >= 'a' && r[0] <= 'z':
		base, size = 'a', 26
	default:
		return false
	}
	var d rune
	for i := 1; i < len(r); i++ {
		a, b := r[i-1]-base, r[i]-base
		if a < 0 || a >= size || b < 0 || b >= size {
			return false
		}
		step := (b - a + size) % size
		if i == 1 {
			if step != 1 && step != size-1 {
				return false
			}
			d = step
		} else if step != d {
			return false
		}
	}
	return true
}

// keyboardWalk is a stretch of a QWERTY layout read along a row, down a column
// or across two rows in turn, forward or backward.
func keyboardWalk(r []rune) bool {
	var buf [64]byte
	key := buf[:0]
	for _, c := range r {
		key = utf8.AppendRune(key, c)
	}
	_, walks := commonTables()
	for _, line := range walks {
		if bytes.Contains(line, key) {
			return true
		}
	}
	return false
}

// keyboardWalks builds the lines a walk is looked for in: each row, the rows
// one after another, two rows interleaved in both orders (1q2w3e, q1w2e3), and
// the columns read from the top down, each also reversed.
func keyboardWalks() [][]byte {
	digits, top, mid, bottom := "1234567890", "qwertyuiop", "asdfghjkl", "zxcvbnm"
	lines := []string{
		digits, top, mid, bottom, "!@#$%^&*()",
		top + mid + bottom,
		digits + top + mid + bottom,
		// Columns, with and without the digit above them.
		"1qaz2wsx3edc4rfv5tgb6yhn7ujm8ik9ol0p",
		"zaq1xsw2cde3vfr4bgt5nhy6mju7ki8lo9",
		"qazwsxedcrfvtgbyhnujmikolp",
		"plokmijnuhbygvtfcrdxeszwaq",
	}
	pairs := [][2]string{
		{digits, top}, {top, digits}, {top, mid}, {mid, top},
		{mid, bottom}, {bottom, mid}, {digits, mid}, {mid, digits},
	}
	for _, p := range pairs {
		var b strings.Builder
		for i := 0; i < len(p[0]) && i < len(p[1]); i++ {
			b.WriteByte(p[0][i])
			b.WriteByte(p[1][i])
		}
		lines = append(lines, b.String())
	}

	out := make([][]byte, 0, 2*len(lines))
	for _, l := range lines {
		out = append(out, []byte(l))
		reversed := []byte(l)
		for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
			reversed[i], reversed[j] = reversed[j], reversed[i]
		}
		out = append(out, reversed)
	}
	return out
}

// longTrivial is the whole of what is asked of an input longer than
// maxAnalysed: one repeated rune, or one sequential run, read without keeping
// anything.
func longTrivial(value string) bool {
	var base, size, prev, first rune
	var d rune
	same, seq, started := true, true, false
	count := 0
	for _, raw := range value {
		r := lowerRune(raw)
		count++
		if !started {
			started, first, prev = true, r, r
			switch {
			case r >= '0' && r <= '9':
				base, size = '0', 10
			case r >= 'a' && r <= 'z':
				base, size = 'a', 26
			default:
				seq = false
			}
			continue
		}
		if r != first {
			same = false
		}
		if seq {
			a, b := prev-base, r-base
			if b < 0 || b >= size {
				seq = false
			} else {
				step := (b - a + size) % size
				switch {
				case count == 2 && (step == 1 || step == size-1):
					d = step
				case count == 2, step != d:
					seq = false
				}
			}
		}
		prev = r
		if !same && !seq {
			return false
		}
	}
	return same || seq
}
