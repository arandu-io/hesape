package qr

import "math/bits"

// Bit patterns of the two BCH codes a QR symbol carries. Format information is
// a (15,5) code masked so that an all-zero format never produces an all-zero
// pattern; version information is an unmasked (18,6) code.
const (
	formatGenerator = 0x537
	formatMask      = 0x5412
	versionGen      = 0x1f25

	// levelHBits is the two-bit format value of error correction level H.
	levelHBits = 0x2

	// maskCount is how many data mask patterns the standard defines.
	maskCount = 8
)

// Penalty weights for the four mask evaluation rules.
const (
	penaltyAdjacent = 3
	penaltyBlock    = 3
	penaltyFinder   = 40
	penaltyBalance  = 10
)

// bchRemainder returns the remainder of data shifted left by degree bits,
// divided by generator over GF(2).
func bchRemainder(data uint32, generator uint32, degree int) uint32 {
	v := data << uint(degree)
	width := bits.Len32(generator)
	for bits.Len32(v) >= width {
		v ^= generator << uint(bits.Len32(v)-width)
	}
	return v
}

// formatInfo returns the fifteen-bit format information pattern for error
// correction level H and the given data mask.
func formatInfo(mask int) uint32 {
	data := uint32(levelHBits<<3 | mask)
	return (data<<10 | bchRemainder(data, formatGenerator, 10)) ^ formatMask
}

// versionInfo returns the eighteen-bit version information pattern.
func versionInfo(number int) uint32 {
	v := uint32(number)
	return v<<12 | bchRemainder(v, versionGen, 12)
}

// maskBit reports whether the data mask inverts the module at the given
// coordinates.
func maskBit(pattern, x, y int) bool {
	switch pattern {
	case 0:
		return (y+x)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (y+x)%3 == 0
	case 4:
		return (y/2+x/3)%2 == 0
	case 5:
		return (y*x)%2+(y*x)%3 == 0
	case 6:
		return ((y*x)%2+(y*x)%3)%2 == 0
	default:
		return ((y+x)%2+(y*x)%3)%2 == 0
	}
}

// matrix holds the modules of a symbol under construction, along with the
// bookkeeping the renderer needs: which modules are function patterns, and
// which codeword each data module belongs to.
type matrix struct {
	v        *version
	size     int
	dark     []bool
	function []bool
	codeword []int32
	blockOf  []int
}

func (m *matrix) set(x, y int, v bool) { m.dark[y*m.size+x] = v }

// reserve marks a module as a function pattern and sets its value.
func (m *matrix) reserve(x, y int, v bool) {
	m.function[y*m.size+x] = true
	m.dark[y*m.size+x] = v
}

// newMatrix builds the function patterns of a symbol and reserves the areas
// the format and version information occupy.
func newMatrix(v *version) *matrix {
	size := v.size()
	m := &matrix{
		v:        v,
		size:     size,
		dark:     make([]bool, size*size),
		function: make([]bool, size*size),
		codeword: make([]int32, size*size),
	}
	for i := range m.codeword {
		m.codeword[i] = -1
	}

	m.placeFinders()
	m.placeAlignment()
	m.placeTiming()
	m.reserveFormatAreas()
	m.placeVersionInfo()
	return m
}

// placeFinders draws the three finder patterns and the light separator that
// surrounds each of them.
func (m *matrix) placeFinders() {
	corners := [3][2]int{{0, 0}, {m.size - 7, 0}, {0, m.size - 7}}
	for _, c := range corners {
		ox, oy := c[0], c[1]
		// The finder itself plus a one-module light border on every side that
		// lies inside the symbol.
		for dy := -1; dy <= 7; dy++ {
			for dx := -1; dx <= 7; dx++ {
				x, y := ox+dx, oy+dy
				if x < 0 || y < 0 || x >= m.size || y >= m.size {
					continue
				}
				inside := dx >= 0 && dx <= 6 && dy >= 0 && dy <= 6
				ring := dx == 0 || dx == 6 || dy == 0 || dy == 6
				eye := dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4
				m.reserve(x, y, inside && (ring || eye))
			}
		}
	}
}

// placeAlignment draws the alignment patterns, skipping the three positions
// that would overlap a finder pattern.
func (m *matrix) placeAlignment() {
	centres := m.v.alignment
	first, last := centres[0], centres[len(centres)-1]
	for _, cy := range centres {
		for _, cx := range centres {
			if (cx == first && cy == first) ||
				(cx == first && cy == last) ||
				(cx == last && cy == first) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					outer := dx == -2 || dx == 2 || dy == -2 || dy == 2
					centre := dx == 0 && dy == 0
					m.reserve(cx+dx, cy+dy, outer || centre)
				}
			}
		}
	}
}

// placeTiming draws the two timing patterns and the single always-dark module.
func (m *matrix) placeTiming() {
	for i := 8; i < m.size-8; i++ {
		dark := i%2 == 0
		m.reserve(i, 6, dark)
		m.reserve(6, i, dark)
	}
	m.reserve(8, m.size-8, true)
}

// reserveFormatAreas marks the modules that carry format information so that
// the data placement walk skips them.
func (m *matrix) reserveFormatAreas() {
	for i := 0; i < 15; i++ {
		x1, y1 := formatPosition1(i)
		x2, y2 := formatPosition2(i, m.size)
		m.reserve(x1, y1, false)
		m.reserve(x2, y2, false)
	}
}

// formatPosition1 returns the module of the format information copy that wraps
// the top-left finder pattern.
func formatPosition1(i int) (x, y int) {
	switch {
	case i < 6:
		return 8, i
	case i < 8:
		return 8, i + 1
	case i == 8:
		return 7, 8
	default:
		return 14 - i, 8
	}
}

// formatPosition2 returns the module of the format information copy that is
// split between the top-right and bottom-left finder patterns.
func formatPosition2(i, size int) (x, y int) {
	if i < 8 {
		return size - 1 - i, 8
	}
	return 8, size - 15 + i
}

// placeVersionInfo draws the two version information blocks.
func (m *matrix) placeVersionInfo() {
	info := versionInfo(m.v.number)
	for i := 0; i < 18; i++ {
		bit := info&(1<<uint(i)) != 0
		m.reserve(i/3, m.size-11+i%3, bit)
		m.reserve(m.size-11+i%3, i/3, bit)
	}
}

// placeData writes the codeword bits along the standard placement walk: pairs
// of columns from right to left, alternating upwards and downwards, skipping
// the vertical timing pattern.
func (m *matrix) placeData(codewords []byte, blockOf []int) {
	m.blockOf = blockOf
	bit := 0
	total := len(codewords) * 8
	upward := true
	for right := m.size - 1; right > 0; right -= 2 {
		if right == 6 {
			right--
		}
		for i := 0; i < m.size; i++ {
			y := i
			if upward {
				y = m.size - 1 - i
			}
			for c := 0; c < 2; c++ {
				x := right - c
				if m.function[y*m.size+x] {
					continue
				}
				if bit < total {
					value := codewords[bit/8]&(0x80>>uint(bit%8)) != 0
					m.set(x, y, value)
					m.codeword[y*m.size+x] = int32(bit / 8)
				}
				bit++
			}
		}
		upward = !upward
	}
}

// applyMask returns the module values with the data mask applied to every
// module that is not a function pattern, and with the format information for
// that mask written in.
func (m *matrix) applyMask(pattern int) []bool {
	out := make([]bool, len(m.dark))
	copy(out, m.dark)
	for y := 0; y < m.size; y++ {
		for x := 0; x < m.size; x++ {
			i := y*m.size + x
			if !m.function[i] && maskBit(pattern, x, y) {
				out[i] = !out[i]
			}
		}
	}
	info := formatInfo(pattern)
	for i := 0; i < 15; i++ {
		bit := info&(1<<uint(i)) != 0
		x1, y1 := formatPosition1(i)
		x2, y2 := formatPosition2(i, m.size)
		out[y1*m.size+x1] = bit
		out[y2*m.size+x2] = bit
	}
	return out
}

// chooseMask applies every data mask and keeps the one with the lowest penalty
// score, resolving a tie by the lower mask number.
func (m *matrix) chooseMask() (pattern int, modules []bool) {
	best := -1
	bestScore := 0
	var bestModules []bool
	for p := 0; p < maskCount; p++ {
		candidate := m.applyMask(p)
		score := penaltyScore(candidate, m.size)
		if best < 0 || score < bestScore {
			best, bestScore, bestModules = p, score, candidate
		}
	}
	return best, bestModules
}

// penaltyScore sums the four mask evaluation rules of the standard.
func penaltyScore(modules []bool, size int) int {
	at := func(x, y int) bool { return modules[y*size+x] }

	score := 0

	// Rule one: runs of five or more modules of the same colour in a row or a
	// column.
	runPenalty := func(length int) int {
		if length < 5 {
			return 0
		}
		return penaltyAdjacent + (length - 5)
	}
	for i := 0; i < size; i++ {
		rowRun, colRun := 1, 1
		for j := 1; j < size; j++ {
			if at(j, i) == at(j-1, i) {
				rowRun++
			} else {
				score += runPenalty(rowRun)
				rowRun = 1
			}
			if at(i, j) == at(i, j-1) {
				colRun++
			} else {
				score += runPenalty(colRun)
				colRun = 1
			}
		}
		score += runPenalty(rowRun) + runPenalty(colRun)
	}

	// Rule two: every two by two block of one colour.
	for y := 0; y < size-1; y++ {
		for x := 0; x < size-1; x++ {
			v := at(x, y)
			if at(x+1, y) == v && at(x, y+1) == v && at(x+1, y+1) == v {
				score += penaltyBlock
			}
		}
	}

	// Rule three: the finder-like one to one to three to one to one ratio with
	// four light modules on either side, in a row or a column.
	pattern := [11]bool{true, false, true, true, true, false, true, false, false, false, false}
	matches := func(get func(int) bool, start int, reversed bool) bool {
		for k := 0; k < 11; k++ {
			want := pattern[k]
			if reversed {
				want = pattern[10-k]
			}
			if get(start+k) != want {
				return false
			}
		}
		return true
	}
	for i := 0; i < size; i++ {
		row := func(j int) bool { return at(j, i) }
		col := func(j int) bool { return at(i, j) }
		for j := 0; j+11 <= size; j++ {
			if matches(row, j, false) || matches(row, j, true) {
				score += penaltyFinder
			}
			if matches(col, j, false) || matches(col, j, true) {
				score += penaltyFinder
			}
		}
	}

	// Rule four: how far the proportion of dark modules strays from one half.
	dark := 0
	for _, v := range modules {
		if v {
			dark++
		}
	}
	percent := dark * 100 / len(modules)
	deviation := percent - 50
	if deviation < 0 {
		deviation = -deviation
	}
	score += deviation / 5 * penaltyBalance

	return score
}
