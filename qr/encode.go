package qr

// byteModeIndicator is the four-bit mode indicator for a byte-mode segment.
const byteModeIndicator = 0x4

// padCodewords are the two codewords alternated to fill the data capacity left
// over after the content and the terminator.
var padCodewords = [2]byte{0xec, 0x11}

// bitWriter appends bits to a byte slice, most significant bit first.
type bitWriter struct {
	bytes []byte
	bits  int
}

// write appends the low n bits of value, most significant of those first.
func (w *bitWriter) write(value uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		if w.bits%8 == 0 {
			w.bytes = append(w.bytes, 0)
		}
		if value&(1<<uint(i)) != 0 {
			w.bytes[w.bits/8] |= 0x80 >> uint(w.bits%8)
		}
		w.bits++
	}
}

// dataCodewords returns the data codewords for the content at the given
// version: one byte-mode segment, a terminator, and the padding that fills the
// version's data capacity.
func dataCodewords(content string, v *version) []byte {
	capacity := v.dataCodewords()

	var w bitWriter
	w.write(byteModeIndicator, 4)
	w.write(uint32(len(content)), v.characterCountBits())
	for i := 0; i < len(content); i++ {
		w.write(uint32(content[i]), 8)
	}

	// Terminator: up to four zero bits, truncated at the capacity.
	if remaining := capacity*8 - w.bits; remaining > 0 {
		if remaining > 4 {
			remaining = 4
		}
		w.write(0, remaining)
	}
	// Pad to the next codeword boundary.
	if rem := w.bits % 8; rem != 0 {
		w.write(0, 8-rem)
	}

	out := w.bytes
	for i := 0; len(out) < capacity; i++ {
		out = append(out, padCodewords[i%2])
	}
	return out
}

// interleaved returns every codeword of the symbol in the order the placement
// walk consumes them, together with the block each codeword belongs to.
//
// Data codewords are taken one per block in turn, then error correction
// codewords the same way.
func interleaved(data []byte, v *version) (codewords []byte, blockOf []int) {
	type block struct {
		data []byte
		ec   []byte
	}

	blocks := make([]block, 0, v.blocks())
	offset := 0
	appendGroup := func(count, size int) {
		for i := 0; i < count; i++ {
			d := data[offset : offset+size]
			offset += size
			blocks = append(blocks, block{data: d, ec: errorCodewords(d, v.ecPerBlock)})
		}
	}
	appendGroup(v.group1Blocks, v.group1Data)
	appendGroup(v.group2Blocks, v.group2Data)

	total := v.totalCodewords()
	codewords = make([]byte, 0, total)
	blockOf = make([]int, 0, total)

	longest := v.group1Data
	if v.group2Data > longest {
		longest = v.group2Data
	}
	for i := 0; i < longest; i++ {
		for b := range blocks {
			if i < len(blocks[b].data) {
				codewords = append(codewords, blocks[b].data[i])
				blockOf = append(blockOf, b)
			}
		}
	}
	for i := 0; i < v.ecPerBlock; i++ {
		for b := range blocks {
			codewords = append(codewords, blocks[b].ec[i])
			blockOf = append(blockOf, b)
		}
	}
	return codewords, blockOf
}
