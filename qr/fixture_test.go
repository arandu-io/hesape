package qr

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A QR symbol has no published test vectors, so the golden files in testdata
// preserve this encoder's module matrices. They were produced by running this
// file with -qr.update.
//
// Redo it with:
//
//	go test ./qr -run TestFixtures -qr.update
//
// decodeRaster in decode_test.go then reads rendered pixels back without using
// renderer geometry, so a shape drawn off-grid fails even when its module
// matrix is right. It shares QR tables and arithmetic with the encoder and is
// therefore a permanent geometry proof, not an independent interoperability
// implementation. TestEveryStyleDecodes runs it over every style at three
// scales during the ordinary test run.
var updateFixtures = flag.Bool("qr.update", false, "rewrite the golden matrix files in testdata")

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
