package qr

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Defaults the renderer applies where an option is left at its zero value.
const (
	defaultForeground   = Color("#000000")
	defaultBackground   = Color("#ffffff")
	defaultQuietZone    = 4
	defaultPixelSize    = 256.0
	defaultRoundness    = 0.6
	defaultDotRoundness = 1.0
	defaultFinderRound  = 1.0
)

// The roundest each shape may be drawn, in modules, before a reader stops
// finding it.
//
// A module may be rounded until it is a circle inscribed in its cell, and the
// eye of a corner pattern until it is a circle too, without a reader losing
// either. The ring around that eye is the one shape that cannot go far: a
// reader confirms a corner pattern along the diagonal through it, and a corner
// radius of a whole module removes the ring from that line entirely.
const (
	maxModuleRadius     = 0.5
	maxFinderRingRadius = 0.5
	maxFinderEyeRadius  = 1.5
)

// MinimumQuietZone is the width, in modules, of the light margin a reader
// needs around the symbol. Cropping it is the most common reason a styled code
// stops scanning, so the renderer refuses anything narrower.
const MinimumQuietZone = 4

// centerBudgetShare is the share of the error correction a reserved center may
// consume. The rest is left to absorb the wear a code meets in the world:
// glare, a fingerprint, a fold, a camera that gives up half the resolution.
const centerBudgetShare = 0.5

// ModuleShape is how a single dark module is drawn.
type ModuleShape int

const (
	// ModuleSquare fills the whole module. It is the default.
	ModuleSquare ModuleShape = iota
	// ModuleRounded fills the module with its corners rounded.
	ModuleRounded
	// ModuleDot fills the module with a circle inscribed in it.
	ModuleDot
)

// FinderShape is how the three corner patterns are drawn.
type FinderShape int

const (
	// FinderSquare draws square corner patterns. It is the default.
	FinderSquare FinderShape = iota
	// FinderRounded rounds the corners of the ring and of the eye.
	FinderRounded
)

// Gradient paints the modules with two stops along a straight line.
type Gradient struct {
	// From is the color at the start of the line.
	From Color
	// To is the color at its end.
	To Color
	// Angle turns the line, in degrees clockwise from left to right.
	Angle float64
}

// Center reserves a square in the middle of the symbol and places the caller's
// own markup in it. The modules under it are not drawn, so the error
// correction has to repair them.
type Center struct {
	// Fraction is the width of the reserved square as a share of the symbol
	// width, between zero and one.
	Fraction float64
	// Content is SVG markup drawn in a coordinate box one unit square. It may
	// use shape, gradient, and text elements, and no attribute that names a
	// resource, carries style declarations, or runs code.
	Content string
}

// Options controls how a symbol is rendered. The zero value renders black
// square modules on white with the quiet zone the standard asks for, so a
// caller that passes nothing gets a code that scans.
type Options struct {
	// Foreground is the color of the modules. Empty means black. It is ignored
	// when Gradient is set.
	Foreground Color
	// Gradient paints the modules instead of Foreground when it is set.
	Gradient *Gradient
	// Background is the color behind the symbol. Empty means white, and
	// Transparent paints nothing.
	Background Color
	// Module is the shape of a single dark module.
	Module ModuleShape
	// ModuleRadius is how round a module is drawn, from zero for a square to
	// one for the roundest a reader still reads. Zero means the default for
	// the shape; a square module is ModuleSquare rather than a radius of zero.
	ModuleRadius float64
	// Connect joins modules that touch into one shape, rounding only the
	// corners that face a light module. It applies to ModuleRounded.
	Connect bool
	// Finder is the shape of the three corner patterns.
	Finder FinderShape
	// FinderRadius is how round the corner patterns are drawn, from zero for
	// square to one for the roundest a reader still finds. Zero means the
	// default; a square corner pattern is FinderSquare rather than a radius of
	// zero.
	FinderRadius float64
	// QuietZone is the width of the light margin in modules. Zero means the
	// default, and anything below MinimumQuietZone is refused.
	QuietZone int
	// PixelSize is the width and height of the rendered element. Zero means
	// the default.
	PixelSize float64
	// Center reserves a square in the middle for the caller's own markup.
	Center *Center
}

// OptionError reports an option this package cannot honor.
type OptionError struct {
	// Field names the option.
	Field string
	// Reason says what is wrong with it.
	Reason string
}

func (e *OptionError) Error() string {
	return fmt.Sprintf("qr: option %s: %s", e.Field, e.Reason)
}

// QuietZoneError reports a quiet zone narrower than a reader tolerates.
type QuietZoneError struct {
	// Modules is the width that was asked for.
	Modules int
	// Minimum is the width a reader needs.
	Minimum int
}

func (e *QuietZoneError) Error() string {
	return fmt.Sprintf("qr: quiet zone of %d modules is below the %d a reader needs", e.Modules, e.Minimum)
}

// CenterTooLargeError reports a reserved center the error correction cannot
// repair. It names the share that was asked for and the largest this symbol
// allows.
type CenterTooLargeError struct {
	// Fraction is the share of the symbol width that was asked for.
	Fraction float64
	// Maximum is the largest share this symbol allows.
	Maximum float64
}

func (e *CenterTooLargeError) Error() string {
	return fmt.Sprintf("qr: reserved center of %.3f of the symbol width destroys more than the error correction repairs, the largest this symbol allows is %.3f",
		e.Fraction, e.Maximum)
}

// settings holds the options after defaults are applied and every value is
// known good.
type settings struct {
	fill        string
	gradient    *Gradient
	gradientID  string
	background  Color
	module      ModuleShape
	radius      float64
	connect     bool
	finder      FinderShape
	finderRing  float64
	finderEye   float64
	quiet       int
	pixel       float64
	centerSpan  int
	centerStart int
	center      *Center
}

// SVG renders the symbol as an SVG element.
//
// It refuses, rather than rendering something that does not scan: a quiet zone
// below MinimumQuietZone, a color pair below MinimumContrast or with the
// modules lighter than the background, a reserved center larger than the error
// correction repairs, and center markup that would reach outside the document.
func (c *Code) SVG(o Options) (string, error) {
	s, err := c.resolve(o)
	if err != nil {
		return "", err
	}

	total := c.size + 2*s.quiet
	var b strings.Builder

	rendering := "crispEdges"
	if s.module != ModuleSquare || s.finder != FinderSquare {
		rendering = "geometricPrecision"
	}
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" role="img" width="%s" height="%s" viewBox="0 0 %d %d" shape-rendering="%s">`,
		num(s.pixel), num(s.pixel), total, total, rendering)

	if s.gradient != nil {
		x1, y1, x2, y2 := gradientLine(s.gradient.Angle, float64(total))
		fmt.Fprintf(&b, `<defs><linearGradient id="%s" gradientUnits="userSpaceOnUse" x1="%s" y1="%s" x2="%s" y2="%s">`,
			s.gradientID, num(x1), num(y1), num(x2), num(y2))
		fmt.Fprintf(&b, `<stop offset="0" stop-color="%s"/><stop offset="1" stop-color="%s"/></linearGradient></defs>`,
			string(s.gradient.From), string(s.gradient.To))
	}

	if s.background != Transparent {
		fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="%s"/>`, total, total, string(s.background))
	}

	if d := c.finderPath(s); d != "" {
		fmt.Fprintf(&b, `<path fill="%s" fill-rule="evenodd" d="%s"/>`, s.fill, d)
	}
	if d := c.modulePath(s); d != "" {
		fmt.Fprintf(&b, `<path fill="%s" d="%s"/>`, s.fill, d)
	}

	if s.center != nil {
		x := float64(s.quiet + s.centerStart)
		fmt.Fprintf(&b, `<svg x="%s" y="%s" width="%d" height="%d" viewBox="0 0 1 1" overflow="hidden">%s</svg>`,
			num(x), num(x), s.centerSpan, s.centerSpan, s.center.Content)
	}

	b.WriteString(`</svg>`)
	return b.String(), nil
}

// resolve applies the defaults and checks every option, so that rendering
// itself cannot fail.
func (c *Code) resolve(o Options) (*settings, error) {
	s := &settings{
		gradient:   o.Gradient,
		background: o.Background,
		module:     o.Module,
		connect:    o.Connect,
		finder:     o.Finder,
		quiet:      o.QuietZone,
		pixel:      o.PixelSize,
		center:     o.Center,
	}

	switch {
	case s.quiet == 0:
		s.quiet = defaultQuietZone
	case s.quiet < MinimumQuietZone:
		return nil, &QuietZoneError{Modules: s.quiet, Minimum: MinimumQuietZone}
	}
	if s.pixel == 0 {
		s.pixel = defaultPixelSize
	}
	if s.pixel < 0 {
		return nil, &OptionError{Field: "PixelSize", Reason: "must be positive"}
	}
	if s.background == "" {
		s.background = defaultBackground
	}

	if o.ModuleRadius < 0 || o.ModuleRadius > 1 {
		return nil, &OptionError{Field: "ModuleRadius", Reason: "must be between 0 and 1"}
	}
	if o.FinderRadius < 0 || o.FinderRadius > 1 {
		return nil, &OptionError{Field: "FinderRadius", Reason: "must be between 0 and 1"}
	}

	roundness := o.ModuleRadius
	switch s.module {
	case ModuleSquare:
		roundness = 0
	case ModuleRounded:
		if roundness == 0 {
			roundness = defaultRoundness
		}
	case ModuleDot:
		if roundness == 0 {
			roundness = defaultDotRoundness
		}
	default:
		return nil, &OptionError{Field: "Module", Reason: "unknown module shape"}
	}
	s.radius = roundness * maxModuleRadius

	finderRoundness := o.FinderRadius
	switch s.finder {
	case FinderSquare:
		finderRoundness = 0
	case FinderRounded:
		if finderRoundness == 0 {
			finderRoundness = defaultFinderRound
		}
	default:
		return nil, &OptionError{Field: "Finder", Reason: "unknown finder shape"}
	}
	s.finderRing = finderRoundness * maxFinderRingRadius
	s.finderEye = finderRoundness * maxFinderEyeRadius

	// The color behind a transparent background is unknown, so contrast is
	// measured against white.
	surface := s.background
	if surface == Transparent {
		surface = defaultBackground
	}

	if s.gradient != nil {
		if err := checkContrast(s.gradient.From, surface, "Gradient.From", "Background"); err != nil {
			return nil, err
		}
		if err := checkContrast(s.gradient.To, surface, "Gradient.To", "Background"); err != nil {
			return nil, err
		}
		s.gradientID = gradientID(s.gradient)
		s.fill = "url(#" + s.gradientID + ")"
	} else {
		fg := o.Foreground
		if fg == "" {
			fg = defaultForeground
		}
		if err := checkContrast(fg, surface, "Foreground", "Background"); err != nil {
			return nil, err
		}
		s.fill = string(fg)
	}

	if s.center != nil {
		if s.center.Fraction <= 0 || s.center.Fraction >= 1 {
			return nil, &OptionError{Field: "Center.Fraction", Reason: "must be between 0 and 1"}
		}
		if err := validateMarkup(s.center.Content); err != nil {
			return nil, err
		}
		span := int(math.Round(float64(c.size) * s.center.Fraction))
		if span < 1 {
			span = 1
		}
		if (c.size-span)%2 != 0 {
			span++
		}
		maxSpan := c.maxCenterSpan()
		if span > maxSpan {
			return nil, &CenterTooLargeError{
				Fraction: s.center.Fraction,
				Maximum:  float64(maxSpan) / float64(c.size),
			}
		}
		s.centerSpan = span
		s.centerStart = (c.size - span) / 2
	}

	return s, nil
}

// maxCenterSpan returns the widest square, in modules, that may be reserved in
// the middle of this symbol.
func (c *Code) maxCenterSpan() int {
	budget := int(float64(c.correctableCodewords()) * centerBudgetShare)
	best := 0
	for span := 1; span <= c.size; span += 2 {
		start := (c.size - span) / 2
		fits := true
		for _, damaged := range c.damagedCodewordsPerBlock(start, start, span) {
			if damaged > budget {
				fits = false
				break
			}
		}
		if !fits {
			break
		}
		best = span
	}
	return best
}

// MaxCenterFraction returns the widest reserved center this symbol allows, as
// a share of its width. It is zero when no center fits.
func (c *Code) MaxCenterFraction() float64 {
	return float64(c.maxCenterSpan()) / float64(c.size)
}

// drawn reports whether the module at the given coordinates is painted by the
// module path: dark, outside the three corner patterns, and outside the
// reserved center.
func (c *Code) drawn(s *settings, x, y int) bool {
	if !c.Module(x, y) {
		return false
	}
	if inFinder(x, y, c.size) {
		return false
	}
	if s.centerSpan > 0 {
		lo, hi := s.centerStart, s.centerStart+s.centerSpan
		if x >= lo && x < hi && y >= lo && y < hi {
			return false
		}
	}
	return true
}

// inFinder reports whether the module lies inside one of the three corner
// patterns, which are drawn as their own shapes.
func inFinder(x, y, size int) bool {
	return (x < 7 && y < 7) ||
		(x >= size-7 && y < 7) ||
		(x < 7 && y >= size-7)
}

// modulePath builds the path that draws every dark module.
func (c *Code) modulePath(s *settings) string {
	var b strings.Builder
	switch s.module {
	case ModuleSquare:
		// Merge each horizontal run into one rectangle, so that adjacent
		// modules never show a seam.
		for y := 0; y < c.size; y++ {
			x := 0
			for x < c.size {
				if !c.drawn(s, x, y) {
					x++
					continue
				}
				start := x
				for x < c.size && c.drawn(s, x, y) {
					x++
				}
				appendRect(&b, float64(s.quiet+start), float64(s.quiet+y), float64(x-start), 1)
			}
		}
	case ModuleDot:
		suffix := circleSuffix(s.radius)
		for y := 0; y < c.size; y++ {
			for x := 0; x < c.size; x++ {
				if c.drawn(s, x, y) {
					fmt.Fprintf(&b, "M%s %s%s", num(float64(s.quiet+x)+0.5-s.radius), num(float64(s.quiet+y)+0.5), suffix)
				}
			}
		}
	case ModuleRounded:
		suffixes := roundedSuffixes(s.radius)
		for y := 0; y < c.size; y++ {
			for x := 0; x < c.size; x++ {
				if !c.drawn(s, x, y) {
					continue
				}
				corners := 0
				if s.connect {
					up := c.drawn(s, x, y-1)
					down := c.drawn(s, x, y+1)
					left := c.drawn(s, x-1, y)
					right := c.drawn(s, x+1, y)
					if !up && !left {
						corners |= cornerTopLeft
					}
					if !up && !right {
						corners |= cornerTopRight
					}
					if !down && !right {
						corners |= cornerBottomRight
					}
					if !down && !left {
						corners |= cornerBottomLeft
					}
				} else {
					corners = cornerTopLeft | cornerTopRight | cornerBottomRight | cornerBottomLeft
				}
				start := float64(s.quiet + x)
				if corners&cornerTopLeft != 0 {
					start += s.radius
				}
				fmt.Fprintf(&b, "M%s %s%s", num(start), num(float64(s.quiet+y)), suffixes[corners])
			}
		}
	}
	return b.String()
}

// Bits naming the four corners of a module, in the order the outline walks
// them.
const (
	cornerTopLeft = 1 << iota
	cornerTopRight
	cornerBottomRight
	cornerBottomLeft
)

// roundedSuffixes returns, for every combination of rounded corners, the path
// text that follows the initial move of a one-module square.
//
// A symbol holds thousands of modules and only sixteen outlines, so the text
// is built once and the module loop writes a move and one of these.
func roundedSuffixes(r float64) [16]string {
	var out [16]string
	arc := func(dx, dy float64) string {
		return fmt.Sprintf("a%s %s 0 0 1 %s %s", num(r), num(r), num(dx), num(dy))
	}
	for c := range out {
		radius := func(bit int) float64 {
			if c&bit != 0 {
				return r
			}
			return 0
		}
		tl, tr := radius(cornerTopLeft), radius(cornerTopRight)
		br, bl := radius(cornerBottomRight), radius(cornerBottomLeft)

		var b strings.Builder
		fmt.Fprintf(&b, "h%s", num(1-tl-tr))
		if tr > 0 {
			b.WriteString(arc(r, r))
		}
		fmt.Fprintf(&b, "v%s", num(1-tr-br))
		if br > 0 {
			b.WriteString(arc(-r, r))
		}
		fmt.Fprintf(&b, "h%s", num(-(1 - br - bl)))
		if bl > 0 {
			b.WriteString(arc(-r, -r))
		}
		fmt.Fprintf(&b, "v%s", num(-(1 - bl - tl)))
		if tl > 0 {
			b.WriteString(arc(r, -r))
		}
		b.WriteString("z")
		out[c] = b.String()
	}
	return out
}

// circleSuffix returns the path text that follows the initial move of a
// circle, which starts at its leftmost point.
func circleSuffix(r float64) string {
	return fmt.Sprintf("a%s %s 0 1 0 %s 0a%s %s 0 1 0 %s 0z",
		num(r), num(r), num(2*r), num(r), num(r), num(-2*r))
}

// finderPath builds the path that draws the three corner patterns: a ring one
// module thick and an eye three modules wide, with the ring hollowed out by
// the even-odd fill rule.
func (c *Code) finderPath(s *settings) string {
	outer := s.finderRing
	inner := outer - 1
	if inner < 0 {
		inner = 0
	}
	eye := s.finderEye

	var b strings.Builder
	for _, corner := range [3][2]int{{0, 0}, {c.size - 7, 0}, {0, c.size - 7}} {
		x := float64(s.quiet + corner[0])
		y := float64(s.quiet + corner[1])
		appendCorneredRect(&b, x, y, 7, 7, outer, outer, outer, outer)
		appendCorneredRect(&b, x+1, y+1, 5, 5, inner, inner, inner, inner)
		appendCorneredRect(&b, x+2, y+2, 3, 3, eye, eye, eye, eye)
	}
	return b.String()
}

// appendRect writes a square-cornered rectangle as a path subpath.
func appendRect(b *strings.Builder, x, y, w, h float64) {
	fmt.Fprintf(b, "M%s %sh%sv%sh%sz", num(x), num(y), num(w), num(h), num(-w))
}

// appendCorneredRect writes a rectangle whose four corners may each be rounded
// or square, in clockwise order from the top left.
func appendCorneredRect(b *strings.Builder, x, y, w, h, tl, tr, br, bl float64) {
	fmt.Fprintf(b, "M%s %s", num(x+tl), num(y))
	fmt.Fprintf(b, "H%s", num(x+w-tr))
	appendCorner(b, tr, x+w, y+tr)
	fmt.Fprintf(b, "V%s", num(y+h-br))
	appendCorner(b, br, x+w-br, y+h)
	fmt.Fprintf(b, "H%s", num(x+bl))
	appendCorner(b, bl, x, y+h-bl)
	fmt.Fprintf(b, "V%s", num(y+tl))
	appendCorner(b, tl, x+tl, y)
	b.WriteString("z")
}

// appendCorner writes one corner: a quarter arc when it is rounded, and
// nothing when it is square, because the preceding line already reached the
// point.
func appendCorner(b *strings.Builder, r, x, y float64) {
	if r <= 0 {
		return
	}
	fmt.Fprintf(b, "A%s %s 0 0 1 %s %s", num(r), num(r), num(x), num(y))
}

// gradientLine returns the two ends of the gradient line for an angle, sized
// so that it spans the whole rendered box.
func gradientLine(angle, size float64) (x1, y1, x2, y2 float64) {
	rad := angle * math.Pi / 180
	dx, dy := math.Cos(rad), math.Sin(rad)
	half := size * (math.Abs(dx) + math.Abs(dy)) / 2
	c := size / 2
	return c - dx*half, c - dy*half, c + dx*half, c + dy*half
}

// gradientID derives a stable element id from the gradient, so that the same
// gradient always renders the same markup and two symbols on one page do not
// collide.
func gradientID(g *Gradient) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%g", g.From, g.To, g.Angle)))
	return "qr-" + hex.EncodeToString(sum[:4])
}

// num formats a coordinate with the shortest text that keeps four decimals.
func num(v float64) string {
	if v == 0 {
		return "0"
	}
	s := strconv.FormatFloat(v, 'f', 4, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	// Path data does not need the zero before the point, and a symbol repeats
	// these numbers thousands of times.
	switch {
	case strings.HasPrefix(s, "0."):
		s = s[1:]
	case strings.HasPrefix(s, "-0."):
		s = "-" + s[2:]
	}
	return s
}
