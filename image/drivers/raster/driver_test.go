package raster_test

import (
	"bytes"
	"context"
	"errors"
	stdimage "image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"sync"
	"testing"

	"github.com/HugoSmits86/nativewebp"
	"github.com/arandu-io/hesape/image"
	"github.com/arandu-io/hesape/image/drivers/raster"
)

const vector = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 10"><defs><style type="text/css">.brand {fill:#f08030;}</style></defs><path class="brand" d="M5 2H15V8H5Z"/></svg>`

func manager(t *testing.T, options raster.Options) *image.ImageManager {
	t.Helper()
	d, err := raster.New(options)
	if err != nil {
		t.Fatal(err)
	}
	m := image.NewImageManager()
	m.Extend("raster", func() (image.Driver, error) { return d, nil })
	m.SetDefaultDriver("raster")
	return m
}

func inputs(t *testing.T) map[string][]byte {
	t.Helper()
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, 20, 10))
	for y := 2; y < 8; y++ {
		for x := 5; x < 15; x++ {
			img.SetNRGBA(x, y, color.NRGBA{240, 128, 48, 255})
		}
	}
	result := map[string][]byte{"svg": []byte(vector)}
	encoders := map[string]func(*bytes.Buffer) error{
		"png":  func(b *bytes.Buffer) error { return png.Encode(b, img) },
		"jpg":  func(b *bytes.Buffer) error { return jpeg.Encode(b, img, nil) },
		"gif":  func(b *bytes.Buffer) error { return gif.Encode(b, img, nil) },
		"webp": func(b *bytes.Buffer) error { return nativewebp.Encode(b, img, nil) },
	}
	for name, encode := range encoders {
		var b bytes.Buffer
		if err := encode(&b); err != nil {
			t.Fatal(err)
		}
		result[name] = b.Bytes()
	}
	return result
}

func TestStaticConversionMatrix(t *testing.T) {
	m := manager(t, raster.Options{})
	for from, source := range inputs(t) {
		for _, to := range []string{"png", "jpg", "gif", "webp"} {
			t.Run(from+"/"+to, func(t *testing.T) {
				original := bytes.Clone(source)
				resized, err := m.FromBytes(source).Resize(10, 5)
				if err != nil {
					t.Fatal(err)
				}
				converted, err := resized.ToFormat(to)
				if err != nil {
					t.Fatal(err)
				}
				out, err := converted.ToBytes(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				decoded, format, err := stdimage.Decode(bytes.NewReader(out))
				if err != nil {
					t.Fatal(err)
				}
				want := to
				if want == "jpg" {
					want = "jpeg"
				}
				if format != want || decoded.Bounds().Dx() != 10 || decoded.Bounds().Dy() != 5 {
					t.Fatalf("wrong output: %s %v", format, decoded.Bounds())
				}
				if !bytes.Equal(source, original) {
					t.Fatal("source was changed")
				}
			})
		}
	}
}

func TestSVGColorsAndTransparency(t *testing.T) {
	m := manager(t, raster.Options{SVGWidth: 40, SVGHeight: 20})
	for _, format := range []string{"webp", "png"} {
		converted, err := m.FromBytes([]byte(vector)).ToFormat(format)
		if err != nil {
			t.Fatal(err)
		}
		out, err := converted.ToBytes(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		img, _, err := stdimage.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 40 || img.Bounds().Dy() != 20 {
			t.Fatal(img.Bounds())
		}
		_, _, _, alpha := img.At(0, 0).RGBA()
		if alpha != 0 {
			t.Fatal("transparent background lost")
		}
		pixel := color.NRGBAModel.Convert(img.At(20, 10)).(color.NRGBA)
		if pixel != (color.NRGBA{240, 128, 48, 255}) {
			t.Fatalf("CSS class color lost: %v", pixel)
		}
	}
}

func TestWebPAlphaRoundTripAndDefaultFormat(t *testing.T) {
	m := manager(t, raster.Options{})
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, 2, 1))
	img.SetNRGBA(1, 0, color.NRGBA{128, 64, 32, 128})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	webp, err := m.FromBytes(b.Bytes()).ToWebp().ToBytes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resized, err := m.FromBytes(webp).Resize(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	out, err := resized.ToBytes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	decoded, format, err := stdimage.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if format != "webp" {
		t.Fatal(format)
	}
	_, _, _, alpha := decoded.At(1, 0).RGBA()
	if alpha != 128*257 {
		t.Fatalf("alpha = %d", alpha)
	}
}

func TestRefusesUnsafeAndUnsupportedSVG(t *testing.T) {
	m := manager(t, raster.Options{})
	for _, body := range []string{
		`<script>alert(1)</script>`, `<foreignObject/>`, `<text>text</text>`, `<use href="#self" id="self"/>`,
		`<image href="https://example.com/image.png"/>`, `<animate/>`, `<path onload="evil()"/>`,
		`<path fill="url(https://example.com/x)"/>`, `<style>@import "remote.css";</style>`, `<filter/>`,
	} {
		source := []byte(`<svg viewBox="0 0 20 10">` + body + `</svg>`)
		if _, err := m.FromBytes(source).ToWebp().ToBytes(context.Background()); !errors.Is(err, image.ErrImage) {
			t.Fatalf("accepted %s: %v", body, err)
		}
	}
	for _, source := range []string{`<svg viewBox="0 0 NaN 2"/>`, `<svg viewBox="0 0 2 2"></svg><svg/>`, `<!DOCTYPE svg [<!ENTITY x "boom">]><svg width="1" height="1"/>`, `<svg><path></svg>`} {
		if _, err := m.FromBytes([]byte(source)).ToWebp().ToBytes(context.Background()); err == nil {
			t.Fatalf("accepted %s", source)
		}
	}
}

func TestLimitsAndCancellation(t *testing.T) {
	m := manager(t, raster.Options{MaxPixels: 100})
	if _, err := m.FromBytes([]byte(vector)).ToWebp().ToBytes(context.Background()); !errors.Is(err, image.ErrTooLarge) {
		t.Fatalf("pixel limit: %v", err)
	}
	m = manager(t, raster.Options{MaxBytes: 8})
	if _, err := m.FromBytes([]byte(vector)).ToWebp().ToBytes(context.Background()); !errors.Is(err, image.ErrTooLarge) {
		t.Fatalf("byte limit: %v", err)
	}
	m = manager(t, raster.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.FromBytes([]byte(vector)).ToWebp().ToBytes(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := raster.New(raster.Options{SVGWidth: 20}); err == nil {
		t.Fatal("accepted incomplete viewport")
	}
}

func TestUnsupportedFormatsAreNotDisguised(t *testing.T) {
	m := manager(t, raster.Options{})
	for _, format := range []string{"avif", "heic", "heif", "bmp"} {
		converted, err := m.FromBytes([]byte(vector)).ToFormat(format)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := converted.ToBytes(context.Background()); err == nil {
			t.Fatal(format)
		}
	}
}

func TestConcurrentDriverAndPipelineOwnership(t *testing.T) {
	d, err := raster.New()
	if err != nil {
		t.Fatal(err)
	}
	p := image.NewImagePipeline()
	p.Output.Format = "webp"
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			d.MaxPixels(1000)
			if _, err := d.Process(context.Background(), []byte(vector), p); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if p.Output.Format != "webp" {
		t.Fatal("pipeline was mutated")
	}
}

func TestAnimatedGIFIsRefused(t *testing.T) {
	m := manager(t, raster.Options{})
	frame := stdimage.NewPaletted(stdimage.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White})
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, &gif.GIF{Image: []*stdimage.Paletted{frame, frame}, Delay: []int{1, 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.FromBytes(b.Bytes()).ToWebp().ToBytes(context.Background()); err == nil {
		t.Fatal("animation was silently flattened")
	}
}

func TestExportedLogoFillRuleAndMetadata(t *testing.T) {
	m := manager(t, raster.Options{})
	for _, rule := range []string{"evenodd", "nonzero"} {
		source := `<?xml version="1.0"?><!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd"><svg xmlns="http://www.w3.org/2000/svg" width="10mm" height="10mm" viewBox="0 0 20 20" style="fill-rule:` + rule + `"><metadata id="exporter"/><path fill="#ff8000" d="M1 1H19V19H1Z M5 5H15V15H5Z"/></svg>`
		out, err := m.FromBytes([]byte(source)).ToWebp().ToBytes(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		img, _, err := stdimage.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		_, _, _, alpha := img.At(10, 10).RGBA()
		if (rule == "evenodd") != (alpha == 0) {
			t.Fatalf("fill rule %s ignored: alpha %d", rule, alpha)
		}
	}
	for _, source := range []string{
		`<svg viewBox="0 0 20 20"><path fill-rule="evenodd" d="M1 1H19V19H1Z"/></svg>`,
		`<svg viewBox="0 0 20 20"><metadata><path d="M1 1H19V19H1Z"/></metadata></svg>`,
	} {
		if _, err := m.FromBytes([]byte(source)).ToWebp().ToBytes(context.Background()); err == nil {
			t.Fatal("unsupported document silently rendered")
		}
	}
}

func TestAnimatedWebPIsRefused(t *testing.T) {
	m := manager(t, raster.Options{})
	// The decoder can read dimensions, but the driver must refuse animation
	// before decoding frames. VP8X flags: animation=2, canvas=2x2.
	b := []byte{'R', 'I', 'F', 'F', 22, 0, 0, 0, 'W', 'E', 'B', 'P', 'V', 'P', '8', 'X', 10, 0, 0, 0, 2, 0, 0, 0, 1, 0, 0, 1, 0, 0}
	if _, err := m.FromBytes(b).ToPng().ToBytes(context.Background()); err == nil {
		t.Fatal("animation accepted")
	}
}

func TestDimensionsAndDominantColor(t *testing.T) {
	d, err := raster.New()
	if err != nil {
		t.Fatal(err)
	}
	for format, source := range inputs(t) {
		w, h, err := d.Dimensions(source)
		if err != nil || w != 20 || h != 10 {
			t.Fatalf("%s dimensions: %dx%d %v", format, w, h, err)
		}
		c, err := d.DominantColor(context.Background(), source)
		if err != nil || len(c) != 7 || c[0] != '#' {
			t.Fatalf("%s color: %q %v", format, c, err)
		}
	}
}

func TestResizedSVGViewBoxOrigin(t *testing.T) {
	m := manager(t, raster.Options{SVGWidth: 40, SVGHeight: 20})
	source := []byte(`<svg viewBox="10 20 20 10"><rect x="15" y="22" width="10" height="6" fill="#f08030"/></svg>`)
	out, err := m.FromBytes(source).ToPng().ToBytes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := stdimage.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, inside := img.At(12, 6).RGBA()
	_, _, _, outside := img.At(35, 18).RGBA()
	if inside != 65535 || outside != 0 {
		t.Fatalf("viewBox origin was not scaled: inside %d outside %d", inside, outside)
	}
}

func FuzzSVGInput(f *testing.F) {
	f.Add([]byte(vector))
	f.Add([]byte(`<svg viewBox="0 0 20 20"><path d="bad"/></svg>`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 2048 {
			return
		}
		d, err := raster.New(raster.Options{MaxBytes: 2048, MaxPixels: 1024})
		if err != nil {
			t.Fatal(err)
		}
		p := image.NewImagePipeline()
		p.Output.Format = "webp"
		_, _ = d.Process(context.Background(), b, p)
	})
}
