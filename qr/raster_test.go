package qr

import (
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// This file rasterises the SVG the renderer emits, so that the decoder in
// decode_test.go reads pixels rather than the module grid the renderer worked
// from. A renderer that draws the right shapes in the wrong places is only
// caught by reading its output back as an image.

// raster is a rendered image: one relative luminance value per pixel, one for
// white and zero for black.
type raster struct {
	w, h   int
	lum    []float64
	scale  float64
	frame  transform
	fills  map[string]float64
	stack  []transform
	buffer []int
}

// transform is the translation and uniform scale that maps user coordinates
// to pixels. The renderer emits no rotation or skew.
type transform struct {
	sx, sy float64
	tx, ty float64
}

func (t transform) apply(x, y float64) (float64, float64) {
	return t.sx*x + t.tx, t.sy*y + t.ty
}

func (r *raster) top() transform { return r.stack[len(r.stack)-1] }

// rasterize renders SVG markup at the given number of pixels per user unit.
func rasterize(markup string, pixelsPerUnit float64) (*raster, error) {
	r := &raster{scale: pixelsPerUnit, fills: map[string]float64{}}
	dec := xml.NewDecoder(strings.NewReader(markup))
	depth := 0
	var gradientID string

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "svg":
				if err := r.startSVG(el, depth); err != nil {
					return nil, err
				}
				depth++
			case "linearGradient":
				gradientID = attr(el, "id")
			case "stop":
				// The darkest stop stands for the whole gradient, which is
				// safe because every stop has to pass the contrast check.
				lum, err := hexLuminance(attr(el, "stop-color"))
				if err != nil {
					return nil, err
				}
				if cur, ok := r.fills["url(#"+gradientID+")"]; !ok || lum < cur {
					r.fills["url(#"+gradientID+")"] = lum
				}
			case "rect":
				if err := r.drawRect(el); err != nil {
					return nil, err
				}
			case "circle":
				if err := r.drawCircle(el); err != nil {
					return nil, err
				}
			case "path":
				if err := r.drawPath(el); err != nil {
					return nil, err
				}
			case "defs", "g", "title", "desc":
			default:
				return nil, fmt.Errorf("raster: unhandled element %q", el.Name.Local)
			}
		case xml.EndElement:
			if el.Name.Local == "svg" {
				depth--
				r.stack = r.stack[:len(r.stack)-1]
			}
		}
	}
	if r.lum == nil {
		return nil, fmt.Errorf("raster: no svg element")
	}
	return r, nil
}

// startSVG sets up the pixel buffer for the outermost element, and pushes the
// nested viewport transform for any element inside it.
func (r *raster) startSVG(el xml.StartElement, depth int) error {
	box := strings.Fields(attr(el, "viewBox"))
	if len(box) != 4 {
		return fmt.Errorf("raster: svg without a viewBox")
	}
	bw, err := strconv.ParseFloat(box[2], 64)
	if err != nil {
		return err
	}
	bh, err := strconv.ParseFloat(box[3], 64)
	if err != nil {
		return err
	}

	if depth == 0 {
		r.w = int(math.Round(bw * r.scale))
		r.h = int(math.Round(bh * r.scale))
		r.lum = make([]float64, r.w*r.h)
		r.buffer = make([]int, r.w*r.h)
		for i := range r.lum {
			r.lum[i] = 1
		}
		r.frame = transform{sx: r.scale, sy: r.scale}
		r.stack = append(r.stack, r.frame)
		return nil
	}

	// A nested viewport maps its own box onto the rectangle it is placed in.
	parent := r.top()
	x := number(attr(el, "x"))
	y := number(attr(el, "y"))
	w := number(attr(el, "width"))
	h := number(attr(el, "height"))
	px, py := parent.apply(x, y)
	r.stack = append(r.stack, transform{
		sx: parent.sx * w / bw,
		sy: parent.sy * h / bh,
		tx: px,
		ty: py,
	})
	return nil
}

func (r *raster) drawRect(el xml.StartElement) error {
	lum, skip, err := r.fillLuminance(el)
	if err != nil || skip {
		return err
	}
	x, y := number(attr(el, "x")), number(attr(el, "y"))
	w, h := number(attr(el, "width")), number(attr(el, "height"))
	ring := [][2]float64{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}
	r.fillRings([][][2]float64{ring}, false, lum)
	return nil
}

func (r *raster) drawCircle(el xml.StartElement) error {
	lum, skip, err := r.fillLuminance(el)
	if err != nil || skip {
		return err
	}
	cx, cy, rad := number(attr(el, "cx")), number(attr(el, "cy")), number(attr(el, "r"))
	ring := make([][2]float64, 0, 64)
	for i := 0; i < 64; i++ {
		a := 2 * math.Pi * float64(i) / 64
		ring = append(ring, [2]float64{cx + rad*math.Cos(a), cy + rad*math.Sin(a)})
	}
	r.fillRings([][][2]float64{ring}, false, lum)
	return nil
}

func (r *raster) drawPath(el xml.StartElement) error {
	lum, skip, err := r.fillLuminance(el)
	if err != nil || skip {
		return err
	}
	rings, err := flattenPath(attr(el, "d"))
	if err != nil {
		return err
	}
	r.fillRings(rings, attr(el, "fill-rule") == "evenodd", lum)
	return nil
}

// fillLuminance resolves the fill of an element, reporting skip when the
// element paints nothing.
func (r *raster) fillLuminance(el xml.StartElement) (lum float64, skip bool, err error) {
	fill := attr(el, "fill")
	switch {
	case fill == "", fill == "none":
		return 0, true, nil
	case strings.HasPrefix(fill, "url(#"):
		v, ok := r.fills[fill]
		if !ok {
			return 0, false, fmt.Errorf("raster: fill %q was never defined", fill)
		}
		return v, false, nil
	}
	v, err := hexLuminance(fill)
	return v, false, err
}

// fillRings paints the area covered by the rings, under the given fill rule,
// in the current transform.
func (r *raster) fillRings(rings [][][2]float64, evenOdd bool, lum float64) {
	t := r.top()
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)

	for _, ring := range rings {
		px := make([][2]float64, len(ring))
		for i, p := range ring {
			x, y := t.apply(p[0], p[1])
			px[i] = [2]float64{x, y}
		}
		x0, y0, x1, y1 := ringBounds(px)
		minX, minY = math.Min(minX, x0), math.Min(minY, y0)
		maxX, maxY = math.Max(maxX, x1), math.Max(maxY, y1)

		for py := clampInt(int(math.Floor(y0)), 0, r.h); py < clampInt(int(math.Ceil(y1))+1, 0, r.h); py++ {
			for pxi := clampInt(int(math.Floor(x0)), 0, r.w); pxi < clampInt(int(math.Ceil(x1))+1, 0, r.w); pxi++ {
				w, crossings := ringWinding(px, float64(pxi)+0.5, float64(py)+0.5)
				if evenOdd {
					r.buffer[py*r.w+pxi] += crossings
				} else {
					r.buffer[py*r.w+pxi] += w
				}
			}
		}
	}

	for py := clampInt(int(math.Floor(minY)), 0, r.h); py < clampInt(int(math.Ceil(maxY))+1, 0, r.h); py++ {
		for pxi := clampInt(int(math.Floor(minX)), 0, r.w); pxi < clampInt(int(math.Ceil(maxX))+1, 0, r.w); pxi++ {
			i := py*r.w + pxi
			acc := r.buffer[i]
			r.buffer[i] = 0
			if evenOdd && acc%2 != 0 || !evenOdd && acc != 0 {
				r.lum[i] = lum
			}
		}
	}
}

func ringBounds(ring [][2]float64) (x0, y0, x1, y1 float64) {
	x0, y0 = math.Inf(1), math.Inf(1)
	x1, y1 = math.Inf(-1), math.Inf(-1)
	for _, p := range ring {
		x0, y0 = math.Min(x0, p[0]), math.Min(y0, p[1])
		x1, y1 = math.Max(x1, p[0]), math.Max(y1, p[1])
	}
	return x0, y0, x1, y1
}

// ringWinding returns the signed winding number and the crossing count of a
// ring around a point, by casting a ray to the right.
func ringWinding(ring [][2]float64, x, y float64) (winding, crossings int) {
	for i := range ring {
		p1 := ring[i]
		p2 := ring[(i+1)%len(ring)]
		if (p1[1] <= y) == (p2[1] <= y) {
			continue
		}
		at := p1[0] + (y-p1[1])/(p2[1]-p1[1])*(p2[0]-p1[0])
		if at <= x {
			continue
		}
		crossings++
		if p2[1] > p1[1] {
			winding++
		} else {
			winding--
		}
	}
	return winding, crossings
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// flattenPath turns path data into closed rings, approximating every arc with
// straight segments.
func flattenPath(d string) ([][][2]float64, error) {
	p := &pathReader{s: d}
	var rings [][][2]float64
	var cur [][2]float64
	var x, y float64

	closeRing := func() {
		if len(cur) > 2 {
			rings = append(rings, cur)
		}
		cur = nil
	}

	for {
		cmd, ok := p.command()
		if !ok {
			break
		}
		switch cmd {
		case 'M':
			closeRing()
			x, y = p.number(), p.number()
			cur = append(cur, [2]float64{x, y})
		case 'H':
			x = p.number()
			cur = append(cur, [2]float64{x, y})
		case 'V':
			y = p.number()
			cur = append(cur, [2]float64{x, y})
		case 'h':
			x += p.number()
			cur = append(cur, [2]float64{x, y})
		case 'v':
			y += p.number()
			cur = append(cur, [2]float64{x, y})
		case 'L':
			x, y = p.number(), p.number()
			cur = append(cur, [2]float64{x, y})
		case 'A', 'a':
			rx, ry := p.number(), p.number()
			p.number() // x-axis rotation, always zero here
			large, sweep := p.number() != 0, p.number() != 0
			ex, ey := p.number(), p.number()
			if cmd == 'a' {
				ex, ey = x+ex, y+ey
			}
			cur = append(cur, arcPoints(x, y, ex, ey, rx, ry, large, sweep)...)
			x, y = ex, ey
		case 'z', 'Z':
			closeRing()
		default:
			return nil, fmt.Errorf("raster: unhandled path command %q", string(cmd))
		}
		if p.err != nil {
			return nil, p.err
		}
	}
	closeRing()
	return rings, nil
}

// arcPoints returns the points along an elliptical arc, from just after its
// start to its end.
func arcPoints(x1, y1, x2, y2, rx, ry float64, large, sweep bool) [][2]float64 {
	if rx == 0 || ry == 0 || (x1 == x2 && y1 == y2) {
		return [][2]float64{{x2, y2}}
	}
	// Endpoint to centre conversion, with no x-axis rotation.
	dx2, dy2 := (x1-x2)/2, (y1-y2)/2
	lambda := dx2*dx2/(rx*rx) + dy2*dy2/(ry*ry)
	if lambda > 1 {
		s := math.Sqrt(lambda)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*dy2*dy2 - ry*ry*dx2*dx2
	den := rx*rx*dy2*dy2 + ry*ry*dx2*dx2
	factor := 0.0
	if den != 0 && num > 0 {
		factor = math.Sqrt(num / den)
	}
	if large == sweep {
		factor = -factor
	}
	cx1, cy1 := factor*rx*dy2/ry, -factor*ry*dx2/rx
	cx, cy := cx1+(x1+x2)/2, cy1+(y1+y2)/2

	angle := func(x, y float64) float64 { return math.Atan2((y-cy)/ry, (x-cx)/rx) }
	a1, a2 := angle(x1, y1), angle(x2, y2)
	delta := a2 - a1
	if sweep && delta < 0 {
		delta += 2 * math.Pi
	}
	if !sweep && delta > 0 {
		delta -= 2 * math.Pi
	}

	steps := int(math.Ceil(math.Abs(delta)/(math.Pi/16))) + 1
	out := make([][2]float64, 0, steps)
	for i := 1; i <= steps; i++ {
		a := a1 + delta*float64(i)/float64(steps)
		out = append(out, [2]float64{cx + rx*math.Cos(a), cy + ry*math.Sin(a)})
	}
	return out
}

// pathReader walks path data one token at a time.
type pathReader struct {
	s   string
	i   int
	err error
}

func (p *pathReader) skip() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == ',' || p.s[p.i] == '\n' || p.s[p.i] == '\t') {
		p.i++
	}
}

func (p *pathReader) command() (byte, bool) {
	p.skip()
	if p.i >= len(p.s) {
		return 0, false
	}
	c := p.s[p.i]
	if c >= '0' && c <= '9' || c == '-' || c == '.' {
		p.err = fmt.Errorf("raster: number where a path command was expected at %d", p.i)
		return 0, false
	}
	p.i++
	return c, true
}

func (p *pathReader) number() float64 {
	p.skip()
	start := p.i
	if p.i < len(p.s) && (p.s[p.i] == '-' || p.s[p.i] == '+') {
		p.i++
	}
	for p.i < len(p.s) && (p.s[p.i] >= '0' && p.s[p.i] <= '9' || p.s[p.i] == '.') {
		p.i++
	}
	if start == p.i {
		p.err = fmt.Errorf("raster: expected a number at %d", start)
		return 0
	}
	v, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil {
		p.err = err
	}
	return v
}

func attr(el xml.StartElement, name string) string {
	for _, a := range el.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func number(s string) float64 {
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// hexLuminance returns the relative luminance of an sRGB hex color.
func hexLuminance(s string) (float64, error) {
	lum, err := relativeLuminance(Color(s), "raster")
	if err != nil {
		return 0, err
	}
	return lum, nil
}
