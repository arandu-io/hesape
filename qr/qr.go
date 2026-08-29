package qr

import (
	"errors"
	"fmt"
)

// ErrEmptyContent is returned by Encode when there is nothing to encode.
var ErrEmptyContent = errors.New("qr: content is empty")

// ContentTooLargeError is returned by Encode when the content does not fit the
// largest supported symbol. The content is never truncated.
type ContentTooLargeError struct {
	// Bytes is the size of the content.
	Bytes int
	// Limit is the largest content size any supported version holds.
	Limit int
}

func (e *ContentTooLargeError) Error() string {
	return fmt.Sprintf("qr: content is %d bytes, the largest supported symbol holds %d", e.Bytes, e.Limit)
}

// Code is an encoded QR symbol: a square grid of dark and light modules,
// without the quiet zone.
type Code struct {
	version  *version
	size     int
	mask     int
	modules  []bool
	codeword []int32
	blockOf  []int
}

// Encode returns the QR symbol carrying content, at error correction level H.
//
// It reports ErrEmptyContent for empty content and a *ContentTooLargeError for
// content larger than the biggest supported symbol.
func Encode(content string) (*Code, error) {
	if content == "" {
		return nil, ErrEmptyContent
	}
	v, err := selectVersion(len(content))
	if err != nil {
		return nil, err
	}

	m := newMatrix(v)
	codewords, blockOf := interleaved(dataCodewords(content, v), v)
	m.placeData(codewords, blockOf)
	mask, modules := m.chooseMask()

	return &Code{
		version:  v,
		size:     v.size(),
		mask:     mask,
		modules:  modules,
		codeword: m.codeword,
		blockOf:  blockOf,
	}, nil
}

// Version returns the symbol version, between 7 and 16.
func (c *Code) Version() int { return c.version.number }

// Size returns the width of the symbol in modules, excluding the quiet zone.
func (c *Code) Size() int { return c.size }

// Mask returns the number of the data mask pattern chosen for this symbol.
func (c *Code) Mask() int { return c.mask }

// Module reports whether the module at column x and row y is dark. It reports
// false for coordinates outside the symbol.
func (c *Code) Module(x, y int) bool {
	if x < 0 || y < 0 || x >= c.size || y >= c.size {
		return false
	}
	return c.modules[y*c.size+x]
}

// correctableCodewords returns, per block, how many wrong codewords the error
// correction can repair.
func (c *Code) correctableCodewords() int { return c.version.ecPerBlock / 2 }

// damagedCodewordsPerBlock counts, for each block of the symbol, how many of
// its codewords have at least one module inside the given square of modules.
func (c *Code) damagedCodewordsPerBlock(x0, y0, span int) []int {
	seen := make(map[int32]bool)
	damaged := make([]int, c.version.blocks())
	for y := y0; y < y0+span; y++ {
		for x := x0; x < x0+span; x++ {
			if x < 0 || y < 0 || x >= c.size || y >= c.size {
				continue
			}
			cw := c.codeword[y*c.size+x]
			if cw < 0 || seen[cw] {
				continue
			}
			seen[cw] = true
			damaged[c.blockOf[cw]]++
		}
	}
	return damaged
}
