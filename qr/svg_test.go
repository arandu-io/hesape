package qr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// styles are the renderings this package promises, from the one a caller gets
// by passing nothing to the one with every option turned on.
func styles(max float64) map[string]Options {
	return map[string]Options{
		"zero value": {},
		"foreground": {Foreground: "#0b3d2e", Background: "#f5f5f4"},
		"connected":  {Module: ModuleRounded, Connect: true, Finder: FinderRounded},
		"roundest": {
			Module: ModuleRounded, Connect: true, ModuleRadius: 1,
			Finder: FinderRounded, FinderRadius: 1,
		},
		"loose rounded": {Module: ModuleRounded, ModuleRadius: 1, Finder: FinderRounded},
		"dots":          {Module: ModuleDot, Finder: FinderRounded},
		"transparent":   {Background: Transparent, Module: ModuleRounded, Connect: true},
		"wide quiet":    {QuietZone: 8},
		"gradient": {
			Gradient: &Gradient{From: "#101820", To: "#0b3d2e", Angle: 45},
			Module:   ModuleRounded, Connect: true, Finder: FinderRounded,
		},
		"everything": {
			Module: ModuleRounded, Connect: true, ModuleRadius: 1,
			Finder: FinderRounded, FinderRadius: 1,
			Gradient:  &Gradient{From: "#0b3d2e", To: "#123c69", Angle: 135},
			PixelSize: 320,
			Center: &Center{
				Fraction: max,
				Content:  `<circle cx="0.5" cy="0.5" r="0.48" fill="#0b3d2e"/>`,
			},
		},
	}
}

func TestZeroOptionsRenderAPlainCode(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	svg, err := c.SVG(Options{})
	if err != nil {
		t.Fatalf("the zero value was refused: %v", err)
	}
	total := c.Size() + 2*MinimumQuietZone
	for _, want := range []string{
		fmt.Sprintf(`viewBox="0 0 %d %d"`, total, total),
		`width="256"`,
		`fill="#ffffff"`,
		`fill="#000000"`,
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("the zero value did not emit %s", want)
		}
	}
}

func TestQuietZoneIsEmittedAndNeverNarrowed(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}

	for _, narrow := range []int{1, 2, 3} {
		_, err := c.SVG(Options{QuietZone: narrow})
		var quiet *QuietZoneError
		if !errors.As(err, &quiet) {
			t.Fatalf("a quiet zone of %d returned %v, want a *QuietZoneError", narrow, err)
		}
		if quiet.Modules != narrow || quiet.Minimum != MinimumQuietZone {
			t.Errorf("error reports %d against %d, want %d against %d",
				quiet.Modules, quiet.Minimum, narrow, MinimumQuietZone)
		}
		message := quiet.Error()
		for _, want := range []string{fmt.Sprint(narrow), fmt.Sprint(MinimumQuietZone)} {
			if !strings.Contains(message, want) {
				t.Errorf("message %q does not name %s", message, want)
			}
		}
	}

	for _, wide := range []int{0, MinimumQuietZone, 8} {
		want := wide
		if want == 0 {
			want = MinimumQuietZone
		}
		svg, err := c.SVG(Options{QuietZone: wide})
		if err != nil {
			t.Fatalf("a quiet zone of %d was refused: %v", wide, err)
		}
		total := c.Size() + 2*want
		if !strings.Contains(svg, fmt.Sprintf(`viewBox="0 0 %d %d"`, total, total)) {
			t.Errorf("a quiet zone of %d did not widen the box to %d", wide, total)
		}
		// The margin has to be light in the image, not only in the box.
		r, err := rasterize(svg, 6)
		if err != nil {
			t.Fatal(err)
		}
		dark := binarize(r)
		margin := want * 6
		for y := 0; y < r.h; y++ {
			for x := 0; x < r.w; x++ {
				inMargin := x < margin || y < margin || x >= r.w-margin || y >= r.h-margin
				if inMargin && dark[y*r.w+x] {
					t.Fatalf("a quiet zone of %d has a dark pixel at %d,%d", want, x, y)
				}
			}
		}
	}
}

func TestContrastIsRefusedWithBothColorsNamed(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		options    Options
		dark, back Color
	}{
		{"pale grey on white", Options{Foreground: "#d0d0d0"}, "#d0d0d0", "#ffffff"},
		{"dark on darker", Options{Foreground: "#202020", Background: "#3a3a3a"}, "#202020", "#3a3a3a"},
		{"first stop too pale", Options{Gradient: &Gradient{From: "#d8d8d8", To: "#101010"}}, "#d8d8d8", "#ffffff"},
		{"second stop too pale", Options{Gradient: &Gradient{From: "#101010", To: "#d8d8d8"}}, "#d8d8d8", "#ffffff"},
		{"transparent is judged against white", Options{Foreground: "#d0d0d0", Background: Transparent}, "#d0d0d0", "#ffffff"},
	}
	for _, tc := range cases {
		_, err := c.SVG(tc.options)
		var contrast *ContrastError
		if !errors.As(err, &contrast) {
			t.Errorf("%s: returned %v, want a *ContrastError", tc.name, err)
			continue
		}
		if contrast.Dark != tc.dark || contrast.Light != tc.back {
			t.Errorf("%s: error names %q and %q, want %q and %q",
				tc.name, contrast.Dark, contrast.Light, tc.dark, tc.back)
		}
		if contrast.Contrast >= contrast.Minimum {
			t.Errorf("%s: error reports contrast %.3f, which is not below %.3f",
				tc.name, contrast.Contrast, contrast.Minimum)
		}
		message := contrast.Error()
		for _, want := range []string{string(tc.dark), string(tc.back)} {
			if !strings.Contains(message, want) {
				t.Errorf("%s: message %q does not name %s", tc.name, message, want)
			}
		}
	}
}

func TestLightModulesOnADarkFieldAreRefused(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.SVG(Options{Foreground: "#ffffff", Background: "#101820"})
	var polarity *PolarityError
	if !errors.As(err, &polarity) {
		t.Fatalf("returned %v, want a *PolarityError", err)
	}
	if !strings.Contains(polarity.Error(), "#ffffff") || !strings.Contains(polarity.Error(), "#101820") {
		t.Errorf("message %q does not name both colors", polarity.Error())
	}
}

func TestContrastAtTheThresholdIsAccepted(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	// The pair just above the threshold is accepted and the one just below is
	// not, so the check is a threshold and not a mood.
	if _, err := c.SVG(Options{Foreground: "#c8c8c8", Background: "#ffffff"}); err != nil {
		t.Fatalf("a pair just above the threshold was refused: %v", err)
	}
	if _, err := c.SVG(Options{Foreground: "#d0d0d0", Background: "#ffffff"}); err == nil {
		t.Fatal("a pair just below the threshold was accepted")
	}
	if _, err := c.SVG(Options{Foreground: "#000", Background: "#fff"}); err != nil {
		t.Fatalf("short hex colors were refused: %v", err)
	}
}

func TestUnreadableColorsAreRefused(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Color{"black", "#12", "#1234567", "#gggggg", "rgb(0,0,0)"} {
		_, err := c.SVG(Options{Foreground: bad})
		var colorErr *ColorError
		if !errors.As(err, &colorErr) {
			t.Errorf("foreground %q returned %v, want a *ColorError", bad, err)
			continue
		}
		if colorErr.Value != bad {
			t.Errorf("error names %q, want %q", colorErr.Value, bad)
		}
		if !strings.Contains(colorErr.Error(), string(bad)) {
			t.Errorf("message %q does not name %q", colorErr.Error(), bad)
		}
	}
	if _, err := c.SVG(Options{Background: "nope"}); err == nil {
		t.Error("an unreadable background was accepted")
	}
}

func TestOptionsOutsideTheirRangeAreRefused(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]Options{
		"module radius above one":  {Module: ModuleRounded, ModuleRadius: 1.5},
		"module radius below zero": {Module: ModuleRounded, ModuleRadius: -0.1},
		"finder radius above one":  {Finder: FinderRounded, FinderRadius: 2},
		"unknown module shape":     {Module: ModuleShape(9)},
		"unknown finder shape":     {Finder: FinderShape(9)},
		"negative pixel size":      {PixelSize: -1},
		"center at zero":           {Center: &Center{Fraction: 0, Content: ""}},
		"center below zero":        {Center: &Center{Fraction: -0.2, Content: ""}},
		"center at one":            {Center: &Center{Fraction: 1, Content: ""}},
	}
	for name, o := range cases {
		_, err := c.SVG(o)
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		var option *OptionError
		if errors.As(err, &option) {
			if !strings.Contains(option.Error(), option.Field) ||
				!strings.Contains(option.Error(), option.Reason) {
				t.Errorf("%s: message %q does not carry the field and the reason", name, option.Error())
			}
		}
	}
}

func TestReservedCenterLargerThanTheCorrectionIsRefused(t *testing.T) {
	for _, content := range []string{otpauthShort, otpauthLong} {
		c, err := Encode(content)
		if err != nil {
			t.Fatal(err)
		}
		max := c.MaxCenterFraction()
		if max <= 0 || max >= 1 {
			t.Fatalf("version %d reports a maximum center of %v", c.Version(), max)
		}

		if _, err := c.SVG(Options{Center: &Center{Fraction: max, Content: ""}}); err != nil {
			t.Errorf("version %d refused a center at its own maximum %.4f: %v", c.Version(), max, err)
		}

		over := max + 3/float64(c.Size())
		_, err = c.SVG(Options{Center: &Center{Fraction: over, Content: ""}})
		var tooLarge *CenterTooLargeError
		if !errors.As(err, &tooLarge) {
			t.Fatalf("version %d: a center of %.4f returned %v, want a *CenterTooLargeError", c.Version(), over, err)
		}
		if tooLarge.Fraction != over {
			t.Errorf("error reports a fraction of %v, want %v", tooLarge.Fraction, over)
		}
		if tooLarge.Maximum != max {
			t.Errorf("error reports a maximum of %v, want %v", tooLarge.Maximum, max)
		}
		message := tooLarge.Error()
		for _, want := range []string{fmt.Sprintf("%.3f", over), fmt.Sprintf("%.3f", max)} {
			if !strings.Contains(message, want) {
				t.Errorf("message %q does not name %s", message, want)
			}
		}
	}
}

func TestReservedCenterLeavesItsModulesUndrawn(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	max := c.MaxCenterFraction()
	svg, err := c.SVG(Options{Center: &Center{Fraction: max, Content: ""}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := rasterize(svg, 6)
	if err != nil {
		t.Fatal(err)
	}
	dark := binarize(r)

	span := int(float64(c.Size())*max + 0.5)
	if (c.Size()-span)%2 != 0 {
		span++
	}
	start := (c.Size() - span) / 2
	for y := start; y < start+span; y++ {
		for x := start; x < start+span; x++ {
			px := (MinimumQuietZone + x) * 6
			py := (MinimumQuietZone + y) * 6
			if dark[(py+3)*r.w+px+3] {
				t.Fatalf("module %d,%d inside the reserved center is painted", x, y)
			}
		}
	}
}

func TestCenterMarkupThatReachesOutIsRefused(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	refused := []string{
		`<script>alert(1)</script>`,
		`<circle onclick="steal()" cx="0.5" cy="0.5" r="0.4" fill="#000"/>`,
		`<rect style="fill:red" width="1" height="1"/>`,
		`<image href="https://example.com/logo.png" width="1" height="1"/>`,
		`<image href="data:image/png;base64,AAAA" width="1" height="1"/>`,
		`<use href="#somewhere"/>`,
		`<style>circle{fill:red}</style>`,
		`<foreignObject width="1" height="1"><p>hello</p></foreignObject>`,
		`<a href="https://example.com"><circle r="0.4" fill="#000"/></a>`,
		`<rect width="1" height="1" fill="url(https://example.com/g.svg#g)"/>`,
		`<!DOCTYPE svg>`,
		`<circle cx=0.5 cy="0.5" r="0.4" fill="#000"/>`,
		`<circle cx="0.5" cy="0.5" r="0.4" fill="#000">`,
		`</circle>`,
		`<circle cx="0.5" fill="#000" r="0.4" cy="0.5"/><!-- unterminated`,
		`<text font-face="evil" x="0.5">a</text>`,
	}
	for _, markup := range refused {
		_, err := c.SVG(Options{Center: &Center{Fraction: 0.1, Content: markup}})
		var markupErr *MarkupError
		if !errors.As(err, &markupErr) {
			t.Errorf("markup %q returned %v, want a *MarkupError", markup, err)
			continue
		}
		if !strings.Contains(markupErr.Error(), markupErr.Reason) {
			t.Errorf("message %q does not carry the reason", markupErr.Error())
		}
	}

	accepted := []string{
		``,
		`<circle cx="0.5" cy="0.5" r="0.45" fill="#0b3d2e"/>`,
		`<g><rect x="0.1" y="0.1" width="0.8" height="0.8" rx="0.2" fill="#101820"/></g>`,
		`<!-- a mark --><path d="M0 0h1v1h-1z" fill="#000000" fill-rule="evenodd"/>`,
		`<defs><linearGradient id="m"><stop offset="0" stop-color="#000"/></linearGradient></defs>` +
			`<rect width="1" height="1" fill="url(#m)"/>`,
		`<text x="0.5" y="0.6" text-anchor="middle" font-size="0.5" fill="#000">A</text>`,
	}
	for _, markup := range accepted {
		if _, err := c.SVG(Options{Center: &Center{Fraction: 0.1, Content: markup}}); err != nil {
			t.Errorf("markup %q was refused: %v", markup, err)
		}
	}
}

func TestRenderedMarkupCarriesNoStyleAndNoScript(t *testing.T) {
	for _, content := range []string{otpauthShort, otpauthLong} {
		c, err := Encode(content)
		if err != nil {
			t.Fatal(err)
		}
		for name, o := range styles(c.MaxCenterFraction()) {
			svg, err := c.SVG(o)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			for _, forbidden := range []string{
				"<style", "style=", "<script", "onload", "javascript:", "data:",
				"href", "@import", "<image", "<use", "<foreignObject", "font-face",
			} {
				if strings.Contains(svg, forbidden) {
					t.Errorf("%s: markup contains %q", name, forbidden)
				}
			}
			// The only reference allowed is a gradient in this same document.
			rest := svg
			for {
				k := strings.Index(rest, "url(")
				if k < 0 {
					break
				}
				rest = rest[k+4:]
				if !strings.HasPrefix(rest, "#") {
					t.Errorf("%s: markup references something outside the document", name)
				}
			}
			if !strings.HasPrefix(svg, "<svg ") || !strings.HasSuffix(svg, "</svg>") {
				t.Errorf("%s: markup is not one svg element", name)
			}
		}
	}
}

func TestGradientIsDeclaredAndUsed(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	g := &Gradient{From: "#101820", To: "#0b3d2e", Angle: 45}
	svg, err := c.SVG(Options{Gradient: g})
	if err != nil {
		t.Fatal(err)
	}
	id := gradientID(g)
	for _, want := range []string{
		`<linearGradient id="` + id + `"`,
		`gradientUnits="userSpaceOnUse"`,
		`stop-color="#101820"`,
		`stop-color="#0b3d2e"`,
		`fill="url(#` + id + `)"`,
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("gradient markup is missing %s", want)
		}
	}
	if strings.Contains(svg, `fill="#101820"`) {
		t.Error("a gradient render still names a flat foreground")
	}

	// A different gradient gets a different identifier, so two codes on one
	// page do not fight over it.
	other := &Gradient{From: "#101820", To: "#0b3d2e", Angle: 90}
	if gradientID(other) == id {
		t.Error("two gradients share an identifier")
	}
}

func TestRenderingIsDeterministic(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	for name, o := range styles(c.MaxCenterFraction()) {
		first, err := c.SVG(o)
		if err != nil {
			t.Fatal(err)
		}
		second, err := c.SVG(o)
		if err != nil {
			t.Fatal(err)
		}
		if first != second {
			t.Errorf("%s: two renders of the same options differ", name)
		}
	}
}

func TestConnectingModulesSquaresTheCornersTheyShare(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	free, err := c.SVG(Options{Module: ModuleRounded})
	if err != nil {
		t.Fatal(err)
	}
	joined, err := c.SVG(Options{Module: ModuleRounded, Connect: true})
	if err != nil {
		t.Fatal(err)
	}
	if free == joined {
		t.Fatal("connecting the modules changed nothing")
	}
	// Every module keeps four arcs when they are drawn apart, and loses some
	// where they touch.
	if a, b := strings.Count(free, "a"), strings.Count(joined, "a"); b >= a {
		t.Errorf("joined modules use %d arcs, apart they use %d", b, a)
	}
	if len(joined) >= len(free) {
		t.Errorf("joined markup is %d bytes, apart it is %d", len(joined), len(free))
	}
}

func TestFinderPatternsAreDrawnAsTheirOwnShape(t *testing.T) {
	c, err := Encode(otpauthShort)
	if err != nil {
		t.Fatal(err)
	}
	svg, err := c.SVG(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(svg, `fill-rule="evenodd"`) {
		t.Error("the corner patterns are not drawn as a ring")
	}
	// The three corner patterns are nine subpaths, and none of their modules
	// appears in the module path.
	settings, err := c.resolve(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(c.finderPath(settings), "M"); got != 9 {
		t.Errorf("the corner patterns are %d subpaths, want 9", got)
	}
	for _, p := range [][2]int{{0, 0}, {3, 3}, {c.Size() - 1, 6}, {6, c.Size() - 1}} {
		if c.drawn(settings, p[0], p[1]) {
			t.Errorf("module %v belongs to a corner pattern and is drawn twice", p)
		}
	}
}
