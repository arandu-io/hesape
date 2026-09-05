// SVG rendering and WebP encoding are opt-in: applications using the standard
// image driver do not carry these codecs or their dependency graph.
module github.com/arandu-io/hesape/image/drivers/raster

go 1.26

require (
	github.com/HugoSmits86/nativewebp v1.3.0
	github.com/arandu-io/hesape v0.25.1
	github.com/fyne-io/oksvg v0.2.0
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef
	github.com/srwiley/scanFT v0.0.0-20220128184157-0d1ee492111f
)

require (
	golang.org/x/image v0.45.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)
