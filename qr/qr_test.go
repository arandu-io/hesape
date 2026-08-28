package qr

import (
	"errors"
	"math/rand"
	"strings"
	"testing"
)

// otpauthShort and otpauthLong bracket the content this package exists for: a
// TOTP enrolment URI with a plain issuer, and one with an encoded issuer, a
// long account, a long secret, and every optional parameter.
const (
	otpauthShort = "otpauth://totp/Arandu:ana@example.com?secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP&issuer=Arandu"
	otpauthLong  = "otpauth://totp/Arandu%20Framework:maria.fernanda.oliveira@empresa-muito-longa.com.br" +
		"?secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXPJBSW" +
		"&issuer=Arandu%20Framework&algorithm=SHA256&digits=8&period=30"
)

func TestVersionTableFillsTheSymbolExactly(t *testing.T) {
	for i := range versions {
		v := &versions[i]
		m := newMatrix(v)
		free := 0
		for _, isFunction := range m.function {
			if !isFunction {
				free++
			}
		}
		want := v.totalCodewords()*8 + v.remainderBits
		if free != want {
			t.Errorf("version %d: %d modules carry data, the table asks for %d codewords and %d remainder bits, which is %d",
				v.number, free, v.totalCodewords(), v.remainderBits, want)
		}
	}
}

func TestVersionCapacitiesRise(t *testing.T) {
	previous := 0
	for i := range versions {
		v := &versions[i]
		got := v.byteCapacity()
		if got <= previous {
			t.Errorf("version %d holds %d bytes, no more than version %d", v.number, got, v.number-1)
		}
		previous = got
		if v.size() != 4*v.number+17 {
			t.Errorf("version %d is %d modules wide", v.number, v.size())
		}
	}
	if got := minVersion().number; got != 7 {
		t.Errorf("smallest supported version is %d, want 7", got)
	}
	if got := maxCapacity(); got != 250 {
		t.Errorf("largest capacity is %d bytes, want 250", got)
	}
}

func TestSelectVersionTakesTheSmallestThatFits(t *testing.T) {
	cases := []struct {
		bytes int
		want  int
	}{
		{1, 7}, {64, 7}, {65, 8}, {84, 8}, {85, 9}, {98, 9},
		{119, 10}, {137, 11}, {155, 12}, {177, 13}, {194, 14}, {220, 15}, {250, 16},
	}
	for _, c := range cases {
		v, err := selectVersion(c.bytes)
		if err != nil {
			t.Fatalf("%d bytes: %v", c.bytes, err)
		}
		if v.number != c.want {
			t.Errorf("%d bytes chose version %d, want %d", c.bytes, v.number, c.want)
		}
	}
}

func TestEncodeRefusesEmptyContent(t *testing.T) {
	if _, err := Encode(""); !errors.Is(err, ErrEmptyContent) {
		t.Fatalf("empty content returned %v, want ErrEmptyContent", err)
	}
}

func TestEncodeRefusesContentThatDoesNotFit(t *testing.T) {
	limit := maxCapacity()

	if _, err := Encode(strings.Repeat("a", limit)); err != nil {
		t.Fatalf("content of exactly %d bytes was refused: %v", limit, err)
	}

	_, err := Encode(strings.Repeat("a", limit+1))
	var tooLarge *ContentTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("content of %d bytes returned %v, want a *ContentTooLargeError", limit+1, err)
	}
	if tooLarge.Bytes != limit+1 || tooLarge.Limit != limit {
		t.Errorf("error reports %d bytes against a limit of %d, want %d and %d",
			tooLarge.Bytes, tooLarge.Limit, limit+1, limit)
	}
	for _, want := range []string{"251", "250"} {
		if !strings.Contains(tooLarge.Error(), want) {
			t.Errorf("error %q does not name %s", tooLarge.Error(), want)
		}
	}
}

func TestEncodeChoosesTheVersionTheContentNeeds(t *testing.T) {
	cases := []struct {
		content string
		version int
	}{
		{otpauthShort, 9},
		{otpauthLong, 15},
	}
	for _, c := range cases {
		code, err := Encode(c.content)
		if err != nil {
			t.Fatalf("%d bytes: %v", len(c.content), err)
		}
		if code.Version() != c.version {
			t.Errorf("%d bytes chose version %d, want %d", len(c.content), code.Version(), c.version)
		}
		if code.Size() != 4*c.version+17 {
			t.Errorf("version %d is %d modules wide", code.Version(), code.Size())
		}
	}
}

func TestModuleOutsideTheSymbolIsLight(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {c.Size(), 0}, {0, c.Size()}} {
		if c.Module(p[0], p[1]) {
			t.Errorf("module %v outside the symbol is dark", p)
		}
	}
	// The three corner patterns are dark at their outer corner and light one
	// module further in, which is the separator.
	if !c.Module(0, 0) || c.Module(7, 7) {
		t.Error("the top left corner pattern is not where it should be")
	}
}

func TestFormatInformationIsAValidBCHCode(t *testing.T) {
	var patterns []uint32
	for level := 0; level < 4; level++ {
		for mask := 0; mask < maskCount; mask++ {
			data := uint32(level<<3 | mask)
			patterns = append(patterns, (data<<10|bchRemainder(data, formatGenerator, 10))^formatMask)
		}
	}
	if len(patterns) != 32 {
		t.Fatalf("built %d patterns, want 32", len(patterns))
	}
	for i, p := range patterns {
		if p > 0x7fff {
			t.Errorf("pattern %d is %#x, wider than fifteen bits", i, p)
		}
		if r := bchRemainder(p^formatMask, formatGenerator, 0); r != 0 {
			t.Errorf("pattern %d leaves remainder %#x", i, r)
		}
	}
	minimum := 16
	for i := range patterns {
		for j := i + 1; j < len(patterns); j++ {
			if d := popcount(patterns[i] ^ patterns[j]); d < minimum {
				minimum = d
			}
		}
	}
	// Seven bits apart is what lets a reader correct three wrong bits in the
	// format information, which is the threshold readFormat applies.
	if minimum != 7 {
		t.Errorf("format patterns are %d bits apart at the closest, want 7", minimum)
	}
	// The level H patterns are the ones the encoder emits.
	for mask := 0; mask < maskCount; mask++ {
		if got, want := formatInfo(mask), patterns[levelHBits*8+mask]; got != want {
			t.Errorf("formatInfo(%d) is %#x, want %#x", mask, got, want)
		}
	}
}

func TestVersionInformationIsAValidBCHCode(t *testing.T) {
	var patterns []uint32
	for i := range versions {
		p := versionInfo(versions[i].number)
		if p>>12 != uint32(versions[i].number) {
			t.Errorf("version %d encodes as %#x, whose data bits are wrong", versions[i].number, p)
		}
		if r := bchRemainder(p, versionGen, 0); r != 0 {
			t.Errorf("version %d leaves remainder %#x", versions[i].number, r)
		}
		patterns = append(patterns, p)
	}
	minimum := 19
	for i := range patterns {
		for j := i + 1; j < len(patterns); j++ {
			if d := popcount(patterns[i] ^ patterns[j]); d < minimum {
				minimum = d
			}
		}
	}
	if minimum < 8 {
		t.Errorf("version patterns are %d bits apart at the closest, want at least 8", minimum)
	}
}

func TestMaskIsTheOneWithTheLowestPenalty(t *testing.T) {
	for _, content := range []string{otpauthShort, otpauthLong} {
		v, err := selectVersion(len(content))
		if err != nil {
			t.Fatal(err)
		}
		m := newMatrix(v)
		codewords, blockOf := interleaved(dataCodewords(content, v), v)
		m.placeData(codewords, blockOf)

		scores := make([]int, maskCount)
		best := 0
		for p := range scores {
			scores[p] = penaltyScore(m.applyMask(p), m.size)
			if scores[p] < scores[best] {
				best = p
			}
		}
		chosen, _ := m.chooseMask()
		if chosen != best {
			t.Errorf("version %d chose mask %d scoring %d, but mask %d scores %d",
				v.number, chosen, scores[chosen], best, scores[best])
		}
		code, err := Encode(content)
		if err != nil {
			t.Fatal(err)
		}
		if code.Mask() != best {
			t.Errorf("Encode chose mask %d, want %d", code.Mask(), best)
		}
	}
}

func TestPenaltyScoresAreWhatTheWeightsSayTheyAre(t *testing.T) {
	// Two grids small enough to score by hand. Any one of the four weights
	// moving changes at least one of the totals, which is what stops the mask
	// choice from being a self-consistent opinion.
	const size = 11

	blank := make([]bool, size*size)
	// Runs: eleven rows and eleven columns of eleven, each 3 + (11 - 5) = 9.
	// Blocks: ten by ten of them, all one color, each 3.
	// Finder-like sequences: none, because there is nothing dark.
	// Balance: no dark modules at all, so fifty off, which is ten steps of ten.
	const wantBlank = 22*9 + 100*3 + 0 + 10*10
	if got := penaltyScore(blank, size); got != wantBlank {
		t.Errorf("a blank grid scores %d, want %d", got, wantBlank)
	}

	patterned := make([]bool, size*size)
	for i, dark := range [11]bool{true, false, true, true, true, false, true, false, false, false, false} {
		patterned[5*size+i] = dark
	}
	// Runs: ten untouched rows of eleven at 9, the sixth row broken into runs
	// shorter than five, six untouched columns at 9, and five columns split
	// into two runs of five at 3.
	// Blocks: eighty away from the sixth row, and six that straddle it where
	// both of its modules are light.
	// Finder-like sequences: the one written into the sixth row.
	// Balance: five dark of a hundred and twenty-one is four percent, which is
	// forty-six off and nine steps of ten.
	const wantPatterned = (10*9 + 6*9 + 5*6) + (80+6)*3 + 40 + 9*10
	if got := penaltyScore(patterned, size); got != wantPatterned {
		t.Errorf("a grid with one finder-like sequence scores %d, want %d", got, wantPatterned)
	}

	// The sequence read backwards costs the same as the sequence read forwards.
	mirrored := make([]bool, size*size)
	for i, dark := range [11]bool{true, false, true, true, true, false, true, false, false, false, false} {
		mirrored[5*size+(10-i)] = dark
	}
	if got, want := penaltyScore(mirrored, size), penaltyScore(patterned, size); got != want {
		t.Errorf("the mirrored sequence scores %d, the sequence scores %d", got, want)
	}
}

func TestGeneratorPolynomialHasTheRightRoots(t *testing.T) {
	for _, degree := range []int{22, 24, 26, 28, 30} {
		g := generatorPolynomial(degree)
		if len(g) != degree+1 {
			t.Fatalf("degree %d produced %d coefficients", degree, len(g))
		}
		if g[0] != 1 {
			t.Errorf("degree %d does not start at one", degree)
		}
		for i := 0; i < degree; i++ {
			var y byte
			for _, c := range g {
				y = gfMul(y, gfExp[i]) ^ c
			}
			if y != 0 {
				t.Errorf("degree %d does not vanish at 2^%d", degree, i)
			}
		}
	}
}

func TestErrorCorrectionRepairsWhatItPromises(t *testing.T) {
	// The decoder in this package's tests is what proves the renderer, so it
	// is proved here first, against blocks the encoder produced.
	random := rand.New(rand.NewSource(20260828))
	for i := range versions {
		v := &versions[i]
		correctable := v.ecPerBlock / 2
		data := make([]byte, v.group1Data)
		for j := range data {
			data[j] = byte(random.Intn(256))
		}
		block := append(append([]byte{}, data...), errorCodewords(data, v.ecPerBlock)...)

		if _, err := reedSolomonCorrect(append([]byte{}, block...), v.ecPerBlock); err != nil {
			t.Fatalf("version %d: a clean block was rejected: %v", v.number, err)
		}

		damaged := append([]byte{}, block...)
		for k := 0; k < correctable; k++ {
			damaged[random.Intn(len(damaged))] ^= byte(1 + random.Intn(255))
		}
		fixed, err := reedSolomonCorrect(damaged, v.ecPerBlock)
		if err != nil {
			t.Fatalf("version %d: %d errors were not repaired: %v", v.number, correctable, err)
		}
		if string(fixed) != string(block) {
			t.Errorf("version %d: repair changed the block", v.number)
		}
	}
}
