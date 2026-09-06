// Package raster adds static SVG input and lossless WebP output to the native
// image pipeline without requiring CGo, Node, external processes or network
// access. JPEG, PNG and static GIF continue through the standard driver.
// WebP encoding is pure Go; it does not load system libraries or a WASM runtime.
//
// Register it explicitly:
//
//	images := image.NewImageManager()
//	images.Extend("raster", func() (image.Driver, error) {
//		return raster.New(raster.Options{SVGWidth: 550, SVGHeight: 444})
//	})
//	images.SetDefaultDriver("raster")
//	webp, err := images.FromBytes(svg).ToWebp().ToBytes(ctx)
//
// WebP output is lossless (VP8L), including alpha, by default. LossyWebP
// explicitly opts into lossy color compression controlled by pipeline Quality
// (image.DefaultQuality when zero), while retaining lossless alpha. Measure
// transfer size and review the chosen viewport before registering an asset.
// PNG also preserves
// alpha. JPEG intentionally flattens transparency using the standard driver.
// The source bytes and the caller's pipeline are never modified.
//
// SVG supports the static path/shape/gradient subset implemented by oksvg,
// including simple CSS classes and inherited per-shape fill rules from
// presentation attributes or inline styles. Nonuniform stylesheet fill rules
// are refused. Unsupported elements, external references,
// scripts, animation, entities and recursive use elements are refused. This
// is not a browser or a complete SVG implementation. Retain originals and
// visually review raster derivatives, especially complex artwork. Text must
// be converted to paths before use. AVIF, HEIC, HEIF and BMP are not added.
// Animated GIF and WebP are refused instead of silently losing their frames.
//
// SVGWidth and SVGHeight select the raster viewport; otherwise the viewBox
// dimensions (or unitless/px width and height) are used. Both must be supplied
// together. The native transformations then run on that raster. Input bytes,
// XML depth, element count and decoded pixels are bounded. As with the core
// driver, transformation output dimensions are application-controlled; do not
// accept arbitrary resize parameters from a request. Codec calls cannot be
// interrupted mid-call; context cancellation is checked between bounded stages
// and SVG paths. Convert trusted build assets once and serve hashed cached
// derivatives, rather than converting on every HTTP request.
package raster
