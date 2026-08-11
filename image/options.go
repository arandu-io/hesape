package image

// DefaultQuality answers ImageOutputOptions::DEFAULT_QUALITY.
//
// It is what the driver encodes with when nobody called
// [Image.Quality]: seventy, the number Illuminate picked.
const DefaultQuality = 70

// ImageOutputOptions answers Illuminate\Image\ImageOutputOptions: what the
// image is written as, once every transformation has run.
//
// Both fields are Illuminate's nullable properties, and their zero value is its
// null -- an empty Format is "encode it as whatever it arrived as", a zero
// Quality is [DefaultQuality]. That is why [ImageOutputOptions.HasChanges]
// exists: a pipeline with no transformations and no output options is a
// pipeline the [Image] can skip entirely, handing back the original bytes
// untouched instead of decoding and re-encoding them for nothing.
type ImageOutputOptions struct {
	// Format is one of the names [Image.ToFormat] accepts: webp, jpg, jpeg,
	// png, gif, avif, heic, bmp. Empty keeps the source format.
	Format string
	// Quality is 1 to 100. Zero means [DefaultQuality]. It reaches only the
	// lossy encoders; PNG and GIF ignore it, exactly as they do in Illuminate.
	Quality int
}

// HasChanges answers ImageOutputOptions::hasChanges(): whether anybody asked
// for a format or a quality.
func (o ImageOutputOptions) HasChanges() bool {
	return o.Format != "" || o.Quality != 0
}

// quality is the number the encoder actually gets.
func (o ImageOutputOptions) quality() int {
	if o.Quality == 0 {
		return DefaultQuality
	}
	return o.Quality
}
