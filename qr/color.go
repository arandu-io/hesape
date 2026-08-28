package qr

import (
	"fmt"
	"math"
	"strings"
)

// Color is an sRGB color written the way an SVG presentation attribute takes
// it: "#rgb" or "#rrggbb", in either case. The empty string selects whatever
// default the option it is assigned to documents.
type Color string

// Transparent leaves the area behind the symbol unpainted. Contrast is then
// measured against white, because that is what a transparent code most often
// ends up on and the surface underneath is the caller's to know.
const Transparent Color = "none"

// MinimumContrast is the smallest difference in relative luminance this
// package accepts between the dark modules and the background.
//
// It is the symbol contrast a QR reader is graded against at the lowest
// passing grade. Relative luminance stands in for the reflectance a grading
// instrument measures, which is the closest a renderer can get without
// knowing the display or the ink.
const MinimumContrast = 0.40

// ColorError reports a color this package cannot read.
type ColorError struct {
	// Value is the offending color.
	Value Color
	// Field names the option it was assigned to.
	Field string
}

func (e *ColorError) Error() string {
	return fmt.Sprintf("qr: %s color %q is not %q or an sRGB hex color such as %q",
		e.Field, string(e.Value), string(Transparent), "#1a1a1a")
}

// ContrastError reports a pair of colors a scanner is not expected to tell
// apart. It names both colors and the contrast they reach.
type ContrastError struct {
	// Dark is the color of the modules.
	Dark Color
	// Light is the color behind them.
	Light Color
	// Contrast is the difference in relative luminance the pair reaches.
	Contrast float64
	// Minimum is the difference the pair had to reach.
	Minimum float64
}

func (e *ContrastError) Error() string {
	return fmt.Sprintf("qr: module color %q against background %q reaches contrast %.2f, below the %.2f a reader needs",
		string(e.Dark), string(e.Light), e.Contrast, e.Minimum)
}

// PolarityError reports a color pair whose modules are lighter than the
// background. A reader expects dark modules on a light field.
type PolarityError struct {
	// Dark is the color assigned to the modules.
	Dark Color
	// Light is the color behind them.
	Light Color
}

func (e *PolarityError) Error() string {
	return fmt.Sprintf("qr: module color %q is lighter than background %q, and a reader expects dark modules on a light field",
		string(e.Dark), string(e.Light))
}

// parseColor reads an sRGB hex color into its three channels, each in the
// range zero to one.
func parseColor(c Color, field string) (r, g, b float64, err error) {
	s := strings.TrimPrefix(string(c), "#")
	switch len(s) {
	case 3:
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6:
	default:
		return 0, 0, 0, &ColorError{Value: c, Field: field}
	}
	var v [3]float64
	for i := 0; i < 3; i++ {
		hi, ok1 := hexDigit(s[i*2])
		lo, ok2 := hexDigit(s[i*2+1])
		if !ok1 || !ok2 {
			return 0, 0, 0, &ColorError{Value: c, Field: field}
		}
		v[i] = float64(hi*16+lo) / 255
	}
	return v[0], v[1], v[2], nil
}

func hexDigit(b byte) (int, bool) {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0'), true
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10, true
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10, true
	}
	return 0, false
}

// relativeLuminance returns the sRGB relative luminance of a color, zero for
// black and one for white.
func relativeLuminance(c Color, field string) (float64, error) {
	r, g, b, err := parseColor(c, field)
	if err != nil {
		return 0, err
	}
	linear := func(v float64) float64 {
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b), nil
}

// checkContrast reports whether a dark color reads against a light one: the
// modules must be the darker of the two, by at least MinimumContrast.
func checkContrast(dark, light Color, darkField, lightField string) error {
	dl, err := relativeLuminance(dark, darkField)
	if err != nil {
		return err
	}
	ll, err := relativeLuminance(light, lightField)
	if err != nil {
		return err
	}
	if dl >= ll {
		return &PolarityError{Dark: dark, Light: light}
	}
	if d := ll - dl; d < MinimumContrast {
		return &ContrastError{Dark: dark, Light: light, Contrast: d, Minimum: MinimumContrast}
	}
	return nil
}
