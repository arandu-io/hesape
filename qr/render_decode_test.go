package qr

import (
	"strconv"
	"strings"
	"testing"
)

// The tests here are the ones that matter. Everything else checks that a
// number is where it should be; these read the rendered markup back as an
// image and get the content out of it.

func TestEveryStyleDecodes(t *testing.T) {
	for name, content := range fixtureContents {
		c, err := Encode(content)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for style, o := range styles(c.MaxCenterFraction()) {
			svg, err := c.SVG(o)
			if err != nil {
				t.Fatalf("%s/%s: %v", name, style, err)
			}
			for _, scale := range []float64{6, 10, 16} {
				r, err := rasterize(svg, scale)
				if err != nil {
					t.Fatalf("%s/%s at %v: %v", name, style, scale, err)
				}
				got, err := decodeRaster(r)
				if err != nil {
					t.Errorf("%s/%s at %v pixels a module: %v", name, style, scale, err)
					continue
				}
				if got != content {
					t.Errorf("%s/%s at %v pixels a module: read back %q", name, style, scale, got)
				}
			}
		}
	}
}

func TestReservedCenterStillDecodes(t *testing.T) {
	for name, content := range fixtureContents {
		c, err := Encode(content)
		if err != nil {
			t.Fatal(err)
		}
		max := c.MaxCenterFraction()
		for _, share := range []float64{0.25, 0.5, 0.75, 1} {
			o := Options{
				Module: ModuleRounded, Connect: true, Finder: FinderRounded,
				Gradient: &Gradient{From: "#101820", To: "#0b3d2e", Angle: 45},
				Center: &Center{
					Fraction: max * share,
					Content:  `<circle cx="0.5" cy="0.5" r="0.5" fill="#101820"/>`,
				},
			}
			svg, err := c.SVG(o)
			if err != nil {
				t.Fatalf("%s at %v of the budget: %v", name, share, err)
			}
			r, err := rasterize(svg, 10)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeRaster(r)
			if err != nil {
				t.Errorf("%s at %v of the budget: %v", name, share, err)
				continue
			}
			if got != content {
				t.Errorf("%s at %v of the budget: read back %q", name, share, got)
			}
		}
	}
}

// moduleOffset is how far the control below pushes the modules off the grid.
//
// It is more than half a module because half a module exactly is the boundary
// case and reads correctly: a shape one module wide, moved half a module,
// still covers the point at the middle of the cell it came from. Measured, a
// symbol read back at ten pixels a module survives an offset of 0.50 and fails
// from 0.55 to 0.60 upwards depending on the module shape.
const moduleOffset = 0.6

// TestDecodingCatchesModulesOffTheGrid is the control on the tests above. A
// renderer that draws the right shapes off the grid still holds the right
// module matrix, so only reading the image back catches it. If this test ever
// passes, the ones above have stopped proving anything.
func TestDecodingCatchesModulesOffTheGrid(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	for style, o := range styles(c.MaxCenterFraction()) {
		svg, err := c.SVG(o)
		if err != nil {
			t.Fatal(err)
		}
		shifted := shiftModulePath(t, svg, moduleOffset)
		if shifted == svg {
			t.Fatalf("%s: the module path was not shifted", style)
		}
		r, err := rasterize(shifted, 10)
		if err != nil {
			t.Fatalf("%s: %v", style, err)
		}
		got, err := decodeRaster(r)
		if err == nil && got == otpauthShort {
			t.Errorf("%s: a symbol whose modules sit %v of a module out still decoded", style, moduleOffset)
		}
	}
}

// shiftModulePath moves every module of the rendered symbol sideways, leaving
// the corner patterns where they are.
func shiftModulePath(t *testing.T, svg string, dx float64) string {
	t.Helper()
	// The module path is the last one in the markup; the corner patterns are
	// the one before it.
	start := strings.LastIndex(svg, `<path fill=`)
	if start < 0 {
		t.Fatal("no path element in the markup")
	}
	open := strings.Index(svg[start:], ` d="`)
	if open < 0 {
		t.Fatal("the last path has no data")
	}
	open += start + len(` d="`)
	end := strings.IndexByte(svg[open:], '"')
	if end < 0 {
		t.Fatal("the last path is unterminated")
	}
	end += open

	var out strings.Builder
	data := svg[open:end]
	for i := 0; i < len(data); {
		if data[i] != 'M' {
			out.WriteByte(data[i])
			i++
			continue
		}
		j := i + 1
		for j < len(data) && data[j] != ' ' {
			j++
		}
		v, err := strconv.ParseFloat(data[i+1:j], 64)
		if err != nil {
			t.Fatalf("could not read a move: %v", err)
		}
		out.WriteString("M" + num(v+dx))
		i = j
	}
	return svg[:open] + out.String() + svg[end:]
}
