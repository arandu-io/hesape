package image

import (
	"bytes"
	stdimage "image"
	"image/color"
	"math"
	"sync"

	"github.com/arandu-io/hesape/image/transformations"
)

// TransformationHandler answers the callable Illuminate registers with
// transformUsing(): it is handed the working canvas and the transformation that
// asked for it, and returns the canvas that replaces it.
//
// The canvas is the standard library's *image.RGBA, alpha premultiplied. A
// handler may return the canvas it was given, modified in place, or a new one
// of a different size.
type TransformationHandler func(canvas *stdimage.RGBA, t transformations.Transformation) (*stdimage.RGBA, error)

// Driver answers Illuminate\Contracts\Image\Driver: the thing that actually
// touches pixels.
//
// Illuminate ships two, GD and Imagick, and both are thin covers over
// Intervention Image, which is a Composer package covering a PHP extension.
// Neither has an equivalent here and neither is missing: this package's own
// driver does the work, in Go, out of image/jpeg, image/png and image/gif in
// the standard library. The interface stays because transformUsing() and
// [ImageManager.Extend] both need something to be an implementation of.
type Driver interface {
	// Process runs the pipeline over the bytes and returns the encoded result.
	Process(contents []byte, pipeline *ImagePipeline) ([]byte, error)
	// Dimensions answers the width and height of the image in the bytes.
	// Illuminate returns array{0: int, 1: int}; two results is that array.
	Dimensions(contents []byte) (int, int, error)
	// DominantColor answers the average colour as "#rrggbb".
	DominantColor(contents []byte) (string, error)
	// TransformUsing registers a handler for one transformation, named by its
	// TransformationName. Illuminate keys the same registry by class-string.
	// It answers the driver, so registrations chain as they do in PHP.
	TransformUsing(transformation string, handler TransformationHandler) Driver
}

// stdDriver is the one driver this package carries.
//
// It is unexported on purpose. Illuminate names its drivers after the PHP
// extension underneath them -- "gd", "imagick" -- and there is no extension
// here to name, so exporting a type would mean inventing a name for it against
// ADR 0044. What a caller needs is reachable without one: [NewImageManager]
// installs it, [ImageManager.Driver] hands it back, and [ImageManager.Extend]
// is how a different one gets in.
type stdDriver struct {
	mu       sync.RWMutex
	handlers map[string]TransformationHandler
}

func newStdDriver() *stdDriver {
	return &stdDriver{handlers: map[string]TransformationHandler{}}
}

// TransformUsing answers InterventionDriver::transformUsing().
func (d *stdDriver) TransformUsing(transformation string, handler TransformationHandler) Driver {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.handlers == nil {
		d.handlers = map[string]TransformationHandler{}
	}
	d.handlers[transformation] = handler
	return d
}

// handlerFor answers InterventionDriver::transformationHandlerFor().
func (d *stdDriver) handlerFor(name string) TransformationHandler {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.handlers[name]
}

// Process answers InterventionDriver::process().
func (d *stdDriver) Process(contents []byte, pipeline *ImagePipeline) ([]byte, error) {
	mediaType := sniffMimeType(contents)
	if !supportedInput(mediaType) {
		return nil, fail("the image format [%s] is not supported", mediaType)
	}

	canvas, sourceFormat, err := decodeCanvas(contents)
	if err != nil {
		return nil, err
	}

	// The orientation is read from the bytes as they arrived, before anything
	// is applied, because re-encoding drops the EXIF block: a pipeline that
	// resized before it oriented would have nothing left to read.
	orientation := exifOrientation(contents)

	for _, t := range pipeline.Transformations {
		if handler := d.handlerFor(t.TransformationName()); handler != nil {
			canvas, err = handler(canvas, t)
			if err != nil {
				return nil, failWith(err, "the [%s] transformation failed", t.TransformationName())
			}
			continue
		}
		canvas, err = d.apply(canvas, t, orientation)
		if err != nil {
			return nil, err
		}
	}

	format := pipeline.Output.Format
	if format == "" {
		format = sourceFormat
	}
	return encodeCanvas(canvas, format, pipeline.Output.quality())
}

// apply is InterventionDriver::process()'s match expression: one transformation
// onto the canvas, or an error naming the one nobody taught this driver.
func (d *stdDriver) apply(canvas *stdimage.RGBA, t transformations.Transformation, orientation int) (*stdimage.RGBA, error) {
	switch v := t.(type) {
	case transformations.Orient:
		return applyOrientation(canvas, orientation), nil
	case transformations.Cover:
		return cover(canvas, v.Width, v.Height), nil
	case transformations.Contain:
		bg, err := resolveBackground(canvas, v.Background)
		if err != nil {
			return nil, err
		}
		return contain(canvas, v.Width, v.Height, bg), nil
	case transformations.Crop:
		return crop(canvas, v.X, v.Y, v.Width, v.Height), nil
	case transformations.Resize:
		return resize(canvas, v.Width, v.Height), nil
	case transformations.Rotate:
		bg, err := resolveBackground(canvas, v.Background)
		if err != nil {
			return nil, err
		}
		return rotate(canvas, v.Angle, bg), nil
	case transformations.Scale:
		return scaleDown(canvas, v.Width, v.Height), nil
	case transformations.Blur:
		return blur(canvas, blurRadius(v.Amount)), nil
	case transformations.Grayscale:
		return grayscale(canvas), nil
	case transformations.Sharpen:
		return sharpen(canvas, v.Amount), nil
	case transformations.FlipVertically:
		return flipVertical(canvas), nil
	case transformations.FlipHorizontally:
		return flipHorizontal(canvas), nil
	default:
		return nil, fail("the image transformation [%s] is not supported", t.TransformationName())
	}
}

// Dimensions answers InterventionDriver::dimensions().
func (d *stdDriver) Dimensions(contents []byte) (int, int, error) {
	cfg, _, err := stdimage.DecodeConfig(bytes.NewReader(contents))
	if err != nil {
		return 0, 0, failWith(err, "unable to determine the dimensions of the image")
	}
	return cfg.Width, cfg.Height, nil
}

// DominantColor answers InterventionDriver::dominantColor().
//
// Illuminate samples it by resizing the image down to a single pixel and
// reading that pixel. Averaging every pixel is the same answer without the
// intermediate canvas, and without the rounding the resize would add.
func (d *stdDriver) DominantColor(contents []byte) (string, error) {
	canvas, _, err := decodeCanvas(contents)
	if err != nil {
		return "", err
	}
	return hex(averageColor(canvas)), nil
}

// resolveBackground answers InterventionDriver::resolveBackground(), including
// the "dominant" sentinel that stands for the image's own average colour.
//
// An empty background is Illuminate's null, which reaches Intervention as its
// default of white -- not as transparency. A rotate that left the corners clear
// would look right in a PNG and black in a JPEG, and Illuminate picked the
// answer that is the same in both.
func resolveBackground(canvas *stdimage.RGBA, background string) (color.RGBA, error) {
	switch background {
	case "":
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}, nil
	case "dominant":
		return averageColor(canvas), nil
	default:
		return parseHexColor(background)
	}
}

// cover fills the box edge to edge, keeping the aspect ratio and cropping what
// overflows from the centre. It is Intervention's cover().
func cover(src *stdimage.RGBA, w, h int) *stdimage.RGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	if sw == 0 || sh == 0 || w < 1 || h < 1 {
		return src
	}
	scale := math.Max(float64(w)/float64(sw), float64(h)/float64(sh))
	rw := maxInt(1, int(math.Round(float64(sw)*scale)))
	rh := maxInt(1, int(math.Round(float64(sh)*scale)))
	resized := resample(src, rw, rh)
	return crop(resized, (rw-w)/2, (rh-h)/2, w, h)
}

// contain fits the whole image inside the box, keeping the aspect ratio, and
// pads the rest with bg so the result is exactly w by h. It is Intervention's
// contain().
func contain(src *stdimage.RGBA, w, h int, bg color.RGBA) *stdimage.RGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	if sw == 0 || sh == 0 || w < 1 || h < 1 {
		return src
	}
	scale := math.Min(float64(w)/float64(sw), float64(h)/float64(sh))
	rw := maxInt(1, int(math.Round(float64(sw)*scale)))
	rh := maxInt(1, int(math.Round(float64(sh)*scale)))
	resized := resample(src, rw, rh)

	dst := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	fill(dst, bg)
	at := stdimage.Rect((w-rw)/2, (h-rh)/2, (w-rw)/2+rw, (h-rh)/2+rh)
	drawOver(dst, at, resized)
	return dst
}

// resize is Intervention's resize(): exactly the dimensions given, aspect ratio
// not kept, and an axis left at zero keeps the size it had.
func resize(src *stdimage.RGBA, w, h int) *stdimage.RGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	if w < 1 {
		w = sw
	}
	if h < 1 {
		h = sh
	}
	return resample(src, w, h)
}

// scaleDown is Intervention's scaleDown(), which Illuminate's Scale maps onto:
// the aspect ratio is kept, the result fits inside the box, and an image
// already smaller than the box is left alone.
func scaleDown(src *stdimage.RGBA, w, h int) *stdimage.RGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	if sw == 0 || sh == 0 {
		return src
	}
	scale := 1.0
	if w > 0 {
		scale = math.Min(scale, float64(w)/float64(sw))
	}
	if h > 0 {
		scale = math.Min(scale, float64(h)/float64(sh))
	}
	if scale >= 1 {
		return src
	}
	return resample(src,
		maxInt(1, int(math.Round(float64(sw)*scale))),
		maxInt(1, int(math.Round(float64(sh)*scale))))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
