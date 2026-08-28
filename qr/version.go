package qr

// version is one row of the capacity table: a QR symbol version at error
// correction level H.
//
// Data codewords are split into at most two groups of blocks. Every block of a
// group holds the same number of data codewords, and every block of the symbol
// carries the same number of error correction codewords.
type version struct {
	number        int
	group1Blocks  int
	group1Data    int
	group2Blocks  int
	group2Data    int
	ecPerBlock    int
	remainderBits int
	alignment     []int
}

// versions is the capacity table at error correction level H, restricted to
// the versions that byte-mode content between 64 and 250 bytes needs. Rows
// outside that range are deliberately absent.
var versions = [...]version{
	{7, 4, 13, 1, 14, 26, 0, []int{6, 22, 38}},
	{8, 4, 14, 2, 15, 26, 0, []int{6, 24, 42}},
	{9, 4, 12, 4, 13, 24, 0, []int{6, 26, 46}},
	{10, 6, 15, 2, 16, 28, 0, []int{6, 28, 50}},
	{11, 3, 12, 8, 13, 24, 0, []int{6, 30, 54}},
	{12, 7, 14, 4, 15, 28, 0, []int{6, 32, 58}},
	{13, 12, 11, 4, 12, 22, 0, []int{6, 34, 62}},
	{14, 11, 12, 5, 13, 24, 3, []int{6, 26, 46, 66}},
	{15, 11, 12, 7, 13, 24, 3, []int{6, 26, 48, 70}},
	{16, 3, 15, 13, 16, 30, 3, []int{6, 26, 50, 74}},
}

// size returns the width of the symbol in modules, excluding the quiet zone.
func (v *version) size() int { return 4*v.number + 17 }

// blocks returns how many blocks the data codewords are split into.
func (v *version) blocks() int { return v.group1Blocks + v.group2Blocks }

// dataCodewords returns how many codewords carry data, across every block.
func (v *version) dataCodewords() int {
	return v.group1Blocks*v.group1Data + v.group2Blocks*v.group2Data
}

// totalCodewords returns how many codewords the symbol holds, data and error
// correction together.
func (v *version) totalCodewords() int {
	return v.dataCodewords() + v.blocks()*v.ecPerBlock
}

// characterCountBits returns the width of the byte-mode character count
// indicator for this version.
func (v *version) characterCountBits() int {
	if v.number <= 9 {
		return 8
	}
	return 16
}

// byteCapacity returns how many content bytes fit in a single byte-mode
// segment, after the mode indicator and the character count indicator.
func (v *version) byteCapacity() int {
	return (v.dataCodewords()*8 - 4 - v.characterCountBits()) / 8
}

// minVersion is the smallest supported version.
func minVersion() *version { return &versions[0] }

// maxCapacity returns the largest content size any supported version holds.
func maxCapacity() int { return versions[len(versions)-1].byteCapacity() }

// selectVersion returns the smallest supported version whose byte-mode
// capacity holds n content bytes. Content below the smallest version's
// capacity still uses that version.
func selectVersion(n int) (*version, error) {
	for i := range versions {
		if n <= versions[i].byteCapacity() {
			return &versions[i], nil
		}
	}
	return nil, &ContentTooLargeError{Bytes: n, Limit: maxCapacity()}
}
