package qr

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A QR symbol has no published test vectors, so the golden files in testdata
// are what an independent reader made of this encoder's output. They were
// produced by running this file with -qr.update, which writes both the module
// matrix and a rendered image of every style, and then by decoding those
// images with a reader that shares no code with this package. The script that
// does the decoding is testdata/crosscheck.py, and it prints what it found.
//
// Redo it with:
//
//	go test ./qr -run TestFixtures -qr.update
//	python3 qr/testdata/crosscheck.py qr/testdata
//
// The images are build output and are not committed; the matrices are.
var updateFixtures = flag.Bool("qr.update", false, "rewrite the golden files and images in testdata")

// fixtureContents are the two ends of the content this package carries.
var fixtureContents = map[string]string{
	"short": otpauthShort,
	"long":  otpauthLong,
}

func TestFixturesMatchTheGoldenMatrices(t *testing.T) {
	for name, content := range fixtureContents {
		c, err := Encode(content)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := describeMatrix(content, c)
		path := filepath.Join("testdata", name+".txt")

		if *updateFixtures {
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: wrote %s", name, path)
			writeStyleImages(t, name, c)
			continue
		}

		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != string(want) {
			t.Errorf("%s: the symbol no longer matches %s", name, path)
			reportFirstDifference(t, string(want), got)
		}
	}
}

// describeMatrix renders a symbol as the text the golden file holds.
func describeMatrix(content string, c *Code) string {
	var b strings.Builder
	fmt.Fprintf(&b, "content: %s\n", content)
	fmt.Fprintf(&b, "bytes: %d\n", len(content))
	fmt.Fprintf(&b, "version: %d\n", c.Version())
	fmt.Fprintf(&b, "size: %d\n", c.Size())
	fmt.Fprintf(&b, "mask: %d\n", c.Mask())
	fmt.Fprintf(&b, "max center: %.4f\n", c.MaxCenterFraction())
	b.WriteString("\n")
	for y := 0; y < c.Size(); y++ {
		for x := 0; x < c.Size(); x++ {
			if c.Module(x, y) {
				b.WriteString("#")
			} else {
				b.WriteString(".")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func reportFirstDifference(t *testing.T, want, got string) {
	t.Helper()
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	for i := range wantLines {
		if i >= len(gotLines) {
			t.Errorf("line %d: the symbol is shorter than the golden file", i+1)
			return
		}
		if wantLines[i] != gotLines[i] {
			t.Errorf("line %d:\n  golden %s\n  now    %s", i+1, wantLines[i], gotLines[i])
			return
		}
	}
}

// writeStyleImages renders every style at several sizes and writes each as a
// grey image, for the cross-check script to read.
func writeStyleImages(t *testing.T, name string, c *Code) {
	t.Helper()
	all := styles(c.MaxCenterFraction())
	names := make([]string, 0, len(all))
	for style := range all {
		names = append(names, style)
	}
	sort.Strings(names)

	for _, style := range names {
		svg, err := c.SVG(all[style])
		if err != nil {
			t.Fatalf("%s/%s: %v", name, style, err)
		}
		for _, scale := range []float64{6, 10, 16} {
			r, err := rasterize(svg, scale)
			if err != nil {
				t.Fatalf("%s/%s: %v", name, style, err)
			}
			file := fmt.Sprintf("%s.%s.s%02d.pgm", name, strings.ReplaceAll(style, " ", "-"), int(scale))
			if err := os.WriteFile(filepath.Join("testdata", file), portableGreyMap(r), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join("testdata", name+".expected"), []byte(fixtureContents[name]), 0o644); err != nil {
		t.Fatal(err)
	}
}

// portableGreyMap serialises a rendered image in the plainest format a reader
// outside Go can open without a library.
func portableGreyMap(r *raster) []byte {
	out := []byte(fmt.Sprintf("P5\n%d %d\n255\n", r.w, r.h))
	for _, v := range r.lum {
		out = append(out, byte(math.Round(math.Max(0, math.Min(1, v))*255)))
	}
	return out
}
