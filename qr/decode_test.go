package qr

import (
	"fmt"
	"math"
	"sort"
)

// This file reads a rendered symbol back the way a reader does: it locates the
// three corner patterns in the pixels, derives the module grid from them,
// samples the modules at their centres, and corrects the result with
// Reed-Solomon before parsing the segment.
//
// Nothing here consults the geometry the renderer used, so a render that draws
// its shapes off the grid fails even though its module matrix is right.

// decodeRaster reads the content of a rendered symbol.
func decodeRaster(r *raster) (string, error) {
	dark := binarize(r)
	finders, module, err := findFinders(dark, r.w, r.h)
	if err != nil {
		return "", err
	}
	grid, size, err := buildGrid(finders, module)
	if err != nil {
		return "", err
	}

	modules := make([]bool, size*size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px, py := grid.sample(x, y)
			ix, iy := int(math.Round(px)), int(math.Round(py))
			if ix < 0 || iy < 0 || ix >= r.w || iy >= r.h {
				return "", fmt.Errorf("decode: module %d,%d falls outside the image", x, y)
			}
			modules[y*size+x] = dark[iy*r.w+ix]
		}
	}
	return decodeModules(modules, size)
}

// binarize splits the image at the midpoint of the luminance it contains.
func binarize(r *raster) []bool {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range r.lum {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	threshold := (lo + hi) / 2
	out := make([]bool, len(r.lum))
	for i, v := range r.lum {
		out[i] = v < threshold
	}
	return out
}

type point struct{ x, y float64 }

// findFinders locates the three corner patterns by their one to one to three
// to one to one run of dark and light, checked along a row and then along the
// column through the same point.
func findFinders(dark []bool, w, h int) ([]point, float64, error) {
	type candidate struct {
		sum    point
		module float64
		count  int
	}
	var clusters []*candidate

	add := func(p point, module float64) {
		for _, c := range clusters {
			cx, cy := c.sum.x/float64(c.count), c.sum.y/float64(c.count)
			if math.Abs(cx-p.x) < module && math.Abs(cy-p.y) < module {
				c.sum.x += p.x
				c.sum.y += p.y
				c.module += module
				c.count++
				return
			}
		}
		clusters = append(clusters, &candidate{sum: p, module: module, count: 1})
	}

	rowAt := func(i, j int) bool { return dark[i*w+j] }
	colAt := func(i, j int) bool { return dark[j*w+i] }

	for y := 0; y < h; y++ {
		for _, run := range ratioRuns(func(j int) bool { return rowAt(y, j) }, w) {
			// Confirm the same ratio down the column through the middle. Two
			// corner patterns can share a column, so the one that matters is
			// the one this row runs through.
			cx := run.centre
			ok := false
			var cy float64
			for _, v := range ratioRuns(func(j int) bool { return colAt(int(cx), j) }, h) {
				if math.Abs(v.module-run.module) >= run.module*0.4 {
					continue
				}
				if math.Abs(v.centre-float64(y)) > run.module*3.5 {
					continue
				}
				ok, cy = true, v.centre
				break
			}
			if ok {
				add(point{cx, cy}, run.module)
			}
		}
	}

	sort.Slice(clusters, func(i, j int) bool { return clusters[i].count > clusters[j].count })
	if len(clusters) < 3 {
		return nil, 0, fmt.Errorf("decode: found %d corner patterns, expected 3", len(clusters))
	}
	if len(clusters) > 12 {
		clusters = clusters[:12]
	}

	centres := make([]point, len(clusters))
	modules := make([]float64, len(clusters))
	for i, c := range clusters {
		centres[i] = point{c.sum.x / float64(c.count), c.sum.y / float64(c.count)}
		modules[i] = c.module / float64(c.count)
	}

	// The data area throws up shapes that hold the same proportion, so the
	// three that matter are the three that sit at the corners of a right
	// isosceles triangle and agree on how wide a module is.
	best, bestScore := [3]int{}, math.Inf(1)
	found := false
	for i := 0; i < len(centres); i++ {
		for j := i + 1; j < len(centres); j++ {
			for k := j + 1; k < len(centres); k++ {
				score := triangleError(centres[i], centres[j], centres[k], modules[i], modules[j], modules[k])
				if score < bestScore {
					best, bestScore, found = [3]int{i, j, k}, score, true
				}
			}
		}
	}
	if !found || bestScore > 0.15 {
		return nil, 0, fmt.Errorf("decode: no three corner patterns form a symbol")
	}

	out := make([]point, 3)
	module := 0.0
	for n, i := range best {
		out[n] = centres[i]
		module += modules[i]
	}
	return out, module / 3, nil
}

// triangleError scores how far three candidates are from the right isosceles
// triangle three corner patterns form, and from agreeing on the module width.
func triangleError(a, b, c point, ma, mb, mc float64) float64 {
	dist := func(p, q point) float64 { return math.Hypot(p.x-q.x, p.y-q.y) }
	sides := []float64{dist(a, b), dist(a, c), dist(b, c)}
	sort.Float64s(sides)
	if sides[2] == 0 {
		return math.Inf(1)
	}
	shape := math.Abs(sides[0]-sides[1])/sides[2] +
		math.Abs(sides[2]-sides[0]*math.Sqrt2)/sides[2]

	widest := math.Max(ma, math.Max(mb, mc))
	narrowest := math.Min(ma, math.Min(mb, mc))
	return shape + (widest-narrowest)/widest
}

type ratioRun struct {
	centre float64
	module float64
}

// ratioRuns returns every place along a line where five runs hold the one to
// one to three to one to one proportion of a corner pattern.
func ratioRuns(at func(int) bool, n int) []ratioRun {
	// Collect run lengths together with the colour they start on.
	var lengths []int
	var colors []bool
	i := 0
	for i < n {
		c := at(i)
		j := i
		for j < n && at(j) == c {
			j++
		}
		lengths = append(lengths, j-i)
		colors = append(colors, c)
		i = j
	}

	var out []ratioRun
	offset := 0
	starts := make([]int, len(lengths))
	for k, l := range lengths {
		starts[k] = offset
		offset += l
	}
	for k := 0; k+5 <= len(lengths); k++ {
		if !colors[k] {
			continue
		}
		total := 0
		for d := 0; d < 5; d++ {
			total += lengths[k+d]
		}
		module := float64(total) / 7
		if module < 1 {
			continue
		}
		want := [5]float64{1, 1, 3, 1, 1}
		ok := true
		for d := 0; d < 5; d++ {
			if math.Abs(float64(lengths[k+d])-want[d]*module) > module*0.5 {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, ratioRun{
				centre: float64(starts[k+2]) + float64(lengths[k+2])/2,
				module: module,
			})
		}
	}
	return out
}

// grid maps module coordinates to pixels.
type grid struct {
	origin       point
	stepX, stepY float64
}

func (g grid) sample(x, y int) (float64, float64) {
	return g.origin.x + float64(x)*g.stepX, g.origin.y + float64(y)*g.stepY
}

// buildGrid turns three corner patterns into the sampling grid, using the
// right angle they form to tell which one is the top left.
func buildGrid(finders []point, module float64) (grid, int, error) {
	dist := func(a, b point) float64 { return math.Hypot(a.x-b.x, a.y-b.y) }
	// The two patterns furthest apart are the diagonal; the third is the
	// corner they turn around.
	worst, wi, wj := -1.0, 0, 0
	for i := 0; i < 3; i++ {
		for j := i + 1; j < 3; j++ {
			if d := dist(finders[i], finders[j]); d > worst {
				worst, wi, wj = d, i, j
			}
		}
	}
	corner := 3 - wi - wj
	tl := finders[corner]
	a, b := finders[wi], finders[wj]
	topRight, bottomLeft := a, b
	if math.Abs(a.y-tl.y) > math.Abs(b.y-tl.y) {
		topRight, bottomLeft = b, a
	}

	span := topRight.x - tl.x
	if span <= 0 || bottomLeft.y-tl.y <= 0 {
		return grid{}, 0, fmt.Errorf("decode: corner patterns are not in the expected corners")
	}
	size := int(math.Round(span/module)) + 7
	if size < 21 || size > 177 || (size-17)%4 != 0 {
		return grid{}, 0, fmt.Errorf("decode: derived symbol size %d is not a valid version", size)
	}

	stepX := span / float64(size-7)
	stepY := (bottomLeft.y - tl.y) / float64(size-7)
	// The centre of the top-left pattern is the centre of module 3,3.
	return grid{
		origin: point{tl.x - 3*stepX, tl.y - 3*stepY},
		stepX:  stepX,
		stepY:  stepY,
	}, size, nil
}

// decodeModules reads a sampled module grid: the format information, then the
// codewords along the placement walk, then the segment they carry.
func decodeModules(modules []bool, size int) (string, error) {
	mask, err := readFormat(modules, size)
	if err != nil {
		return "", err
	}

	number := (size - 17) / 4
	var v *version
	for i := range versions {
		if versions[i].number == number {
			v = &versions[i]
		}
	}
	if v == nil {
		return "", fmt.Errorf("decode: version %d is outside the supported range", number)
	}

	// The function patterns are the same for every symbol of a version, so
	// rebuilding them says which modules carry data.
	layout := newMatrix(v)

	codewords := make([]byte, v.totalCodewords())
	bit := 0
	upward := true
	for right := size - 1; right > 0; right -= 2 {
		if right == 6 {
			right--
		}
		for i := 0; i < size; i++ {
			y := i
			if upward {
				y = size - 1 - i
			}
			for c := 0; c < 2; c++ {
				x := right - c
				if layout.function[y*size+x] {
					continue
				}
				if bit < len(codewords)*8 {
					value := modules[y*size+x]
					if maskBit(mask, x, y) {
						value = !value
					}
					if value {
						codewords[bit/8] |= 0x80 >> uint(bit%8)
					}
				}
				bit++
			}
		}
		upward = !upward
	}

	data, err := correctBlocks(codewords, v)
	if err != nil {
		return "", err
	}
	return readSegment(data, v)
}

// readFormat recovers the data mask from the format information, correcting
// it against the thirty-two patterns the code allows.
func readFormat(modules []bool, size int) (int, error) {
	var raw uint32
	for i := 0; i < 15; i++ {
		x, y := formatPosition1(i)
		if modules[y*size+x] {
			raw |= 1 << uint(i)
		}
	}

	best, bestDistance := -1, 16
	for level := 0; level < 4; level++ {
		for mask := 0; mask < maskCount; mask++ {
			data := uint32(level<<3 | mask)
			want := (data<<10 | bchRemainder(data, formatGenerator, 10)) ^ formatMask
			if d := popcount(want ^ raw); d < bestDistance {
				best, bestDistance = level<<3|mask, d
			}
		}
	}
	if bestDistance > 3 {
		return 0, fmt.Errorf("decode: format information is unreadable")
	}
	if best>>3 != levelHBits {
		return 0, fmt.Errorf("decode: error correction level %d, expected H", best>>3)
	}
	return best & 7, nil
}

func popcount(v uint32) int {
	n := 0
	for v != 0 {
		n += int(v & 1)
		v >>= 1
	}
	return n
}

// correctBlocks undoes the interleaving, repairs each block with Reed-Solomon,
// and returns the data codewords in their original order.
func correctBlocks(codewords []byte, v *version) ([]byte, error) {
	sizes := make([]int, 0, v.blocks())
	for i := 0; i < v.group1Blocks; i++ {
		sizes = append(sizes, v.group1Data)
	}
	for i := 0; i < v.group2Blocks; i++ {
		sizes = append(sizes, v.group2Data)
	}

	blockData := make([][]byte, len(sizes))
	blockEC := make([][]byte, len(sizes))
	for i := range blockData {
		blockData[i] = make([]byte, 0, sizes[i])
		blockEC[i] = make([]byte, 0, v.ecPerBlock)
	}

	longest := v.group1Data
	if v.group2Data > longest {
		longest = v.group2Data
	}
	pos := 0
	for i := 0; i < longest; i++ {
		for b := range blockData {
			if i < sizes[b] {
				blockData[b] = append(blockData[b], codewords[pos])
				pos++
			}
		}
	}
	for i := 0; i < v.ecPerBlock; i++ {
		for b := range blockEC {
			blockEC[b] = append(blockEC[b], codewords[pos])
			pos++
		}
	}

	var out []byte
	for b := range blockData {
		full := append(append([]byte{}, blockData[b]...), blockEC[b]...)
		fixed, err := reedSolomonCorrect(full, v.ecPerBlock)
		if err != nil {
			return nil, fmt.Errorf("decode: block %d: %w", b, err)
		}
		out = append(out, fixed[:len(blockData[b])]...)
	}
	return out, nil
}

// readSegment parses the single byte-mode segment the encoder writes.
func readSegment(data []byte, v *version) (string, error) {
	r := &bitReader{data: data}
	mode := r.read(4)
	if mode != byteModeIndicator {
		return "", fmt.Errorf("decode: mode indicator %d, expected byte mode", mode)
	}
	n := int(r.read(v.characterCountBits()))
	if n > len(data) {
		return "", fmt.Errorf("decode: segment claims %d bytes, block holds %d", n, len(data))
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(r.read(8))
	}
	if r.err != nil {
		return "", r.err
	}
	return string(out), nil
}

type bitReader struct {
	data []byte
	pos  int
	err  error
}

func (r *bitReader) read(n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		if r.pos >= len(r.data)*8 {
			r.err = fmt.Errorf("decode: ran out of bits")
			return v
		}
		v <<= 1
		if r.data[r.pos/8]&(0x80>>uint(r.pos%8)) != 0 {
			v |= 1
		}
		r.pos++
	}
	return v
}

// reedSolomonCorrect repairs a codeword block, returning it with up to half
// the error correction codewords' worth of errors removed.
//
// It works from the syndromes, which is the opposite direction to the
// polynomial division the encoder does.
func reedSolomonCorrect(block []byte, ec int) ([]byte, error) {
	syndromes := make([]byte, ec)
	clean := true
	for j := 0; j < ec; j++ {
		var y byte
		for _, c := range block {
			y = gfMul(y, gfExp[j]) ^ c
		}
		syndromes[j] = y
		if y != 0 {
			clean = false
		}
	}
	if clean {
		return block, nil
	}

	lambda := berlekampMassey(syndromes)
	positions := chienSearch(lambda, len(block))
	if len(positions) == 0 || len(positions) != len(lambda)-1 {
		return nil, fmt.Errorf("found %d error positions for a locator of degree %d", len(positions), len(lambda)-1)
	}

	omega := polyMul(syndromes, lambda)
	if len(omega) > ec {
		omega = omega[:ec]
	}
	derivative := make([]byte, 0, len(lambda)/2)
	for i := 1; i < len(lambda); i += 2 {
		derivative = append(derivative, lambda[i])
	}

	out := append([]byte{}, block...)
	for _, exponent := range positions {
		x := gfExp[exponent]
		xInv := gfInverse(x)
		num := polyEvalLowFirst(omega, xInv)
		den := polyEvalLowFirst(derivative, gfMul(xInv, xInv))
		if den == 0 {
			return nil, fmt.Errorf("error locator has a repeated root")
		}
		magnitude := gfMul(gfMul(x, num), gfInverse(den))
		index := len(block) - 1 - exponent
		if index < 0 || index >= len(out) {
			return nil, fmt.Errorf("error position %d is outside the block", exponent)
		}
		out[index] ^= magnitude
	}

	for j := 0; j < ec; j++ {
		var y byte
		for _, c := range out {
			y = gfMul(y, gfExp[j]) ^ c
		}
		if y != 0 {
			return nil, fmt.Errorf("block still has errors after correction")
		}
	}
	return out, nil
}

// berlekampMassey returns the error locator polynomial, coefficients from the
// constant term upwards.
func berlekampMassey(syndromes []byte) []byte {
	lambda := []byte{1}
	previous := []byte{1}
	l, shift := 0, 1
	var lastDiscrepancy byte = 1

	for n := 0; n < len(syndromes); n++ {
		d := syndromes[n]
		for i := 1; i <= l && i < len(lambda); i++ {
			d ^= gfMul(lambda[i], syndromes[n-i])
		}
		switch {
		case d == 0:
			shift++
		case 2*l <= n:
			previousLambda := append([]byte{}, lambda...)
			scale := gfMul(d, gfInverse(lastDiscrepancy))
			lambda = polyAddShifted(lambda, previous, scale, shift)
			l = n + 1 - l
			previous = previousLambda
			lastDiscrepancy = d
			shift = 1
		default:
			scale := gfMul(d, gfInverse(lastDiscrepancy))
			lambda = polyAddShifted(lambda, previous, scale, shift)
			shift++
		}
	}
	// Trim the zero coefficients the recursion may leave on top.
	for len(lambda) > 1 && lambda[len(lambda)-1] == 0 {
		lambda = lambda[:len(lambda)-1]
	}
	return lambda
}

// polyAddShifted returns a + scale * x^shift * b, low coefficient first.
func polyAddShifted(a, b []byte, scale byte, shift int) []byte {
	out := make([]byte, max(len(a), len(b)+shift))
	copy(out, a)
	for i, c := range b {
		out[i+shift] ^= gfMul(c, scale)
	}
	return out
}

// chienSearch returns the exponents at which the error locator vanishes.
func chienSearch(lambda []byte, length int) []int {
	var out []int
	for i := 0; i < length; i++ {
		if polyEvalLowFirst(lambda, gfInverse(gfExp[i])) == 0 {
			out = append(out, i)
		}
	}
	return out
}

// polyEvalLowFirst evaluates a polynomial whose first coefficient is the
// constant term.
func polyEvalLowFirst(p []byte, x byte) byte {
	var y byte
	for i := len(p) - 1; i >= 0; i-- {
		y = gfMul(y, x) ^ p[i]
	}
	return y
}

// polyMul multiplies two polynomials given low coefficient first.
func polyMul(a, b []byte) []byte {
	out := make([]byte, len(a)+len(b)-1)
	for i, x := range a {
		if x == 0 {
			continue
		}
		for j, y := range b {
			out[i+j] ^= gfMul(x, y)
		}
	}
	return out
}

func gfInverse(a byte) byte {
	if a == 0 {
		return 0
	}
	return gfExp[255-int(gfLog[a])]
}
