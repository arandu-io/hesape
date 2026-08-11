package transformations

// Transformation answers Illuminate\Contracts\Image\Transformation.
//
// In PHP it is an empty marker interface: a class implements it and the driver
// switches on `instanceof`. An empty interface in Go is `any`, which every type
// satisfies, so the marker needs a method -- and the method it got is the one
// the driver needs anyway.
//
// TransformationName answers what `Blur::class` answers in Illuminate: the key
// a per-driver handler is registered under with ImageManager.TransformUsing,
// and the tag the driver switches on. It is the PHP class basename, so the
// registration reads the same on both sides:
//
//	// Illuminate: Image::transformUsing('gd', Blur::class, $callback)
//	manager.TransformUsing("std", "Blur", handler)
//
// A type outside this package may implement it, and that is how a custom
// transformation reaches a driver that was taught to handle it. A driver that
// meets a name it does not know returns an error rather than dropping the
// transformation on the floor -- a resize that silently did not happen is worse
// than one that failed.
type Transformation interface {
	// TransformationName is the PHP class basename: "Blur", "Cover", "Crop".
	TransformationName() string
}

// Blur answers Illuminate\Image\Transformations\Blur.
type Blur struct {
	// Amount is Illuminate's $amount, 0 to 100. Illuminate\Image\Image.Blur
	// clamps it to that range before it gets here.
	Amount int
}

// TransformationName answers Blur::class.
func (Blur) TransformationName() string { return "Blur" }

// Contain answers Illuminate\Image\Transformations\Contain: the whole image
// scaled to fit inside the box, padded with Background to exactly Width by
// Height.
type Contain struct {
	Width  int
	Height int
	// Background is a CSS-style hex colour ("#ffffff", "fff", "ffffffff") or
	// the sentinel "dominant", which Illuminate expands to the image's own
	// dominant colour. Empty is Illuminate's null and means opaque white.
	Background string
}

// TransformationName answers Contain::class.
func (Contain) TransformationName() string { return "Contain" }

// Cover answers Illuminate\Image\Transformations\Cover: the box filled
// edge to edge, aspect ratio kept, whatever overflows cropped from the centre.
type Cover struct {
	Width  int
	Height int
}

// TransformationName answers Cover::class.
func (Cover) TransformationName() string { return "Cover" }

// Crop answers Illuminate\Image\Transformations\Crop.
type Crop struct {
	Width  int
	Height int
	X      int
	Y      int
}

// TransformationName answers Crop::class.
func (Crop) TransformationName() string { return "Crop" }

// FlipHorizontally answers Illuminate\Image\Transformations\FlipHorizontally:
// left becomes right.
type FlipHorizontally struct{}

// TransformationName answers FlipHorizontally::class.
func (FlipHorizontally) TransformationName() string { return "FlipHorizontally" }

// FlipVertically answers Illuminate\Image\Transformations\FlipVertically: top
// becomes bottom.
type FlipVertically struct{}

// TransformationName answers FlipVertically::class.
func (FlipVertically) TransformationName() string { return "FlipVertically" }

// Grayscale answers Illuminate\Image\Transformations\Grayscale.
type Grayscale struct{}

// TransformationName answers Grayscale::class.
func (Grayscale) TransformationName() string { return "Grayscale" }

// Orient answers Illuminate\Image\Transformations\Orient: the EXIF orientation
// tag read and applied, so a photograph taken with the phone on its side is
// stored the way it was seen.
type Orient struct{}

// TransformationName answers Orient::class.
func (Orient) TransformationName() string { return "Orient" }

// Resize answers Illuminate\Image\Transformations\Resize: exactly Width by
// Height, aspect ratio not kept.
type Resize struct {
	// Width is Illuminate's ?int $width. Zero is its null: the axis is left as
	// it is, and the other one moves alone. Illuminate\Image\Image.Resize
	// refuses the call where both are absent.
	Width int
	// Height is Illuminate's ?int $height. Zero is its null.
	Height int
}

// TransformationName answers Resize::class.
func (Resize) TransformationName() string { return "Resize" }

// Rotate answers Illuminate\Image\Transformations\Rotate.
type Rotate struct {
	// Angle is degrees clockwise, as Illuminate documents it.
	Angle float64
	// Background fills the corners the rotation leaves empty. Same spelling as
	// [Contain.Background], including the "dominant" sentinel; empty is
	// Illuminate's null and means transparent.
	Background string
}

// TransformationName answers Rotate::class.
func (Rotate) TransformationName() string { return "Rotate" }

// Scale answers Illuminate\Image\Transformations\Scale: scaled down to fit
// inside the box, aspect ratio kept, never scaled up.
//
// Illuminate maps it onto Intervention's scaleDown(), and the "never up" is the
// whole difference from Resize.
type Scale struct {
	// Width is Illuminate's ?int $width. Zero is its null: that axis does not
	// constrain the result.
	Width int
	// Height is Illuminate's ?int $height. Zero is its null.
	Height int
}

// TransformationName answers Scale::class.
func (Scale) TransformationName() string { return "Scale" }

// Sharpen answers Illuminate\Image\Transformations\Sharpen.
type Sharpen struct {
	// Amount is Illuminate's $amount, 0 to 100.
	Amount int
}

// TransformationName answers Sharpen::class.
func (Sharpen) TransformationName() string { return "Sharpen" }
