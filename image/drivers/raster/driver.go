package raster

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	stdimage "image"
	"image/png"
	"sync"

	"github.com/HugoSmits86/nativewebp"
	"github.com/arandu-io/hesape/image"
)

// Options bounds inputs and selects the SVG raster viewport. Zero values use
// 8 MiB input, image.DefaultMaxPixels, and the SVG's own dimensions.
type Options struct {
	MaxBytes  int
	MaxPixels int
	SVGWidth  int
	SVGHeight int
}

type driver struct {
	base    image.Driver
	mu      sync.RWMutex
	options Options
}

// New creates an optional static raster driver. It can be returned directly
// from an ImageManager.Extend creator. Options are copied, not retained.
func New(options ...Options) (image.Driver, error) {
	if len(options) > 1 {
		return nil, fail("expected at most one options value")
	}
	o := Options{}
	if len(options) == 1 {
		o = options[0]
	}
	if o.MaxBytes < 0 || o.MaxPixels < 0 || o.SVGWidth < 0 || o.SVGHeight < 0 || (o.SVGWidth == 0) != (o.SVGHeight == 0) {
		return nil, fail("invalid raster limits or SVG viewport")
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = 8 << 20
	}
	if o.MaxPixels == 0 {
		o.MaxPixels = image.DefaultMaxPixels
	}
	if o.SVGWidth != 0 {
		if err := checkPixels(o.SVGWidth, o.SVGHeight, o.MaxPixels); err != nil {
			return nil, err
		}
	}
	base, err := image.NewImageManager().Driver()
	if err != nil {
		return nil, err
	}
	base.MaxPixels(o.MaxPixels)
	return &driver{base: base, options: o}, nil
}

func fail(format string, args ...any) error {
	return fmt.Errorf("%w: %s", image.ErrImage, fmt.Sprintf(format, args...))
}

func checkPixels(w, h, limit int) error {
	if w <= 0 || h <= 0 {
		return fail("invalid image dimensions")
	}
	if w > limit/h {
		return fmt.Errorf("%w: %dx%d exceeds %d pixels", image.ErrTooLarge, w, h, limit)
	}
	return nil
}

func (d *driver) settings() Options { d.mu.RLock(); defer d.mu.RUnlock(); return d.options }

func (d *driver) MaxPixels(pixels int) image.Driver {
	if pixels <= 0 {
		pixels = image.DefaultMaxPixels
	}
	d.mu.Lock()
	d.options.MaxPixels = pixels
	d.mu.Unlock()
	d.base.MaxPixels(pixels)
	return d
}

func (d *driver) TransformUsing(name string, handler image.TransformationHandler) image.Driver {
	d.base.TransformUsing(name, handler)
	return d
}

func isSVG(b []byte) bool {
	b = bytes.TrimSpace(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf}))
	return len(b) > 0 && b[0] == '<'
}
func isWebP(b []byte) bool {
	return len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP"
}

func (d *driver) Dimensions(b []byte) (int, int, error) {
	o := d.settings()
	if len(b) > o.MaxBytes {
		return 0, 0, fmt.Errorf("%w: input byte limit", image.ErrTooLarge)
	}
	if isSVG(b) {
		return svgDimensions(context.Background(), b, o)
	}
	c, _, err := stdimage.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return 0, 0, fail("read dimensions: %v", err)
	}
	if err := checkPixels(c.Width, c.Height, o.MaxPixels); err != nil {
		return 0, 0, err
	}
	return c.Width, c.Height, nil
}

func (d *driver) normalize(ctx context.Context, b []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, _, err := d.Dimensions(b); err != nil {
		return nil, err
	}
	var img stdimage.Image
	var err error
	switch {
	case isSVG(b):
		img, err = renderSVG(ctx, b, d.settings())
	case isWebP(b):
		// The animation flag lives in the VP8X header. Never silently flatten.
		if len(b) >= 21 && string(b[12:16]) == "VP8X" && b[20]&2 != 0 {
			return nil, fail("animated WebP is not supported")
		}
		img, err = nativewebp.Decode(bytes.NewReader(b))
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		// Count image descriptors structurally without decoding every frame.
		if err := staticGIF(b); err != nil {
			return nil, err
		}
		return b, nil
	default:
		return b, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: decode raster: %w", image.ErrImage, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fail("encode intermediate PNG: %v", err)
	}
	return out.Bytes(), nil
}

func (d *driver) Process(ctx context.Context, b []byte, pipeline *image.ImagePipeline) ([]byte, error) {
	if pipeline == nil {
		return nil, fail("nil image pipeline")
	}
	wantWebP := pipeline.Output.Format == "webp" || (pipeline.Output.Format == "" && isWebP(b))
	input, err := d.normalize(ctx, b)
	if err != nil {
		return nil, err
	}
	p := *pipeline
	if wantWebP {
		p.Output.Format = "png"
	}
	out, err := d.base.Process(ctx, input, &p)
	if err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !wantWebP {
		return out, nil
	}
	canvas, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		return nil, fail("decode transformed PNG: %v", err)
	}
	var encoded bytes.Buffer
	if err := nativewebp.Encode(&encoded, canvas, nil); err != nil {
		return nil, fail("encode WebP: %v", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func (d *driver) DominantColor(ctx context.Context, b []byte) (string, error) {
	input, err := d.normalize(ctx, b)
	if err != nil {
		return "", err
	}
	return d.base.DominantColor(ctx, input)
}

func staticGIF(b []byte) error {
	if len(b) < 13 {
		return fail("truncated GIF")
	}
	i := 13
	if b[10]&128 != 0 {
		i += 3 << ((b[10] & 7) + 1)
	}
	frames := 0
	for i < len(b) {
		tag := b[i]
		i++
		switch tag {
		case 0x3b:
			if frames != 1 {
				return fail("GIF must have exactly one frame")
			}
			return nil
		case 0x21:
			i++ // extension label
		case 0x2c:
			frames++
			if frames > 1 {
				return fail("animated GIF is not supported")
			}
			if i+9 > len(b) {
				return fail("truncated GIF descriptor")
			}
			w, h := int(binary.LittleEndian.Uint16(b[i+4:])), int(binary.LittleEndian.Uint16(b[i+6:]))
			if w == 0 || h == 0 {
				return fail("invalid GIF frame")
			}
			packed := b[i+8]
			i += 9
			if packed&128 != 0 {
				i += 3 << ((packed & 7) + 1)
			}
			i++ // LZW minimum code size
		default:
			return fail("invalid GIF block")
		}
		for {
			if i >= len(b) {
				return fail("truncated GIF block")
			}
			n := int(b[i])
			i++
			if n == 0 {
				break
			}
			i += n
		}
	}
	return fail("missing GIF trailer")
}
