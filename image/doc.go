// Package image is Illuminate\Image: an image described by what should be done
// to it, and done to it once, when somebody asks for the result.
//
// It was written against the clone in laravel_illuminate/image, tag v13.25.0 --
// Image.php, ImageManager.php, ImagePipeline.php, ImageOutputOptions.php and
// ImageException.php, plus Contracts\Image\Driver from
// reference_laravel/framework, which the clone of contracts does not carry.
// Nothing came from anywhere else.
//
// The shape is Illuminate's, unchanged. [ImageManager] is where images come
// from, [Image] is one image and every fluent call on it returns a new one, and
// the pixels are touched exactly once, by a [Driver], at [Image.ToBytes]:
//
//	images := image.NewImageManager()
//
//	img, err := images.FromUpload(upload)
//	thumb, err := img.Orient().Cover(400, 300).ToJpg().Quality(80).
//		Store(ctx, grant, disk, "avatars")
//
// # No GD, no Imagick, no Intervention
//
// Illuminate ships two drivers, GdDriver and ImagickDriver, and both are twenty
// lines over InterventionDriver, which is a cover over the intervention/image
// Composer package, which is a cover over a PHP extension. Four layers exist
// because PHP cannot resize an image without one.
//
// Go can. The driver here is this package, on image/jpeg, image/png and
// image/gif from the standard library -- which are not a dependency, so the
// module root still declares one (ADR 0048). The [Driver] interface stays,
// because transformUsing() and [ImageManager.Extend] both need something to be
// an implementation of, and because a driver for a format this one cannot write
// is a real thing to add later.
//
// # Which formats
//
// Read: JPEG, PNG, GIF. Written: JPEG, PNG, GIF.
//
// [Image.ToFormat] accepts every name Illuminate accepts -- webp, jpg, jpeg,
// png, gif, avif, heic, heif, bmp -- and the four with no encoder in the
// standard library fail at the encode, with the format named. That is the
// honest arrangement: the alternative is [Image.ToWebp] handing back a JPEG,
// which is a lie that leaves the building as a wrong Content-Type. The same
// goes for reading: a WebP arriving at [Image.ToBytes] is refused by name
// rather than half-decoded.
//
// [Image.MimeType] and [Image.Extension] know the wider list, because they read
// what a file is rather than what this package can do with it -- an image that
// only needs storing never goes near the driver, and its type and extension are
// still right.
//
// # Resizing, and why it looks the way it does
//
// [Image.Resize], [Image.Cover], [Image.Contain] and [Image.Scale] resample
// with a triangle filter -- bilinear -- applied as two separable passes, and
// with one addition that matters: when the image is being made smaller, the
// filter's support is widened by the scale factor, which turns the same code
// into an area average.
//
// Plain bilinear reads at most two source pixels per output pixel. Reducing a
// 4000-pixel photograph to 400 with it ignores nine of every ten columns, and
// what comes out is not soft but wrong: brick walls turn into moire, small text
// turns into gravel. Nearest-neighbour, which is what an afternoon's work
// produces, is worse still. Widening the support costs a few lines and is the
// difference between a thumbnail somebody ships and one somebody complains
// about.
//
// [Image.Rotate] samples bilinearly too, except at the three right angles,
// where it copies pixels instead.
//
// # Orient, and the photograph that arrives sideways
//
// [Image.Orient] reads the EXIF orientation tag out of the bytes as they
// arrived and turns the image to match. A phone does not rotate its sensor when
// the person rotates the phone: it stores the frame as the sensor read it and
// writes down which way was up. Every upload form that skips this shows half
// its portrait photographs lying down, and it is the most reported bug any
// upload form has.
//
// The tag is read before any transformation runs, because re-encoding drops the
// EXIF block: a pipeline that resized first would have nothing left to read.
// The output carries no EXIF, which is the point -- the turn is baked into the
// pixels, so every viewer agrees.
//
// # Storing is authorized, like everything else
//
// [Image.Store] and [ImageManager.FromStorage] take an auth.Grant and a [Disk].
// Illuminate resolves the disk from the container and needs no grant; there is
// no container here (ADR 0001) and there is no path to customer data without a
// policy (RULE 17), including the read. The tenant the image lands under comes
// off the Grant and from nowhere else (RULE 14).
//
// [Disk] and [DiskReader] are interfaces rather than imports of the filesystem
// package, so that this package sits underneath it. A *filesystem.Disk is a
// [Disk] with no adapter.
//
// # What Illuminate has and this does not
//
// ImageServiceProvider (register, provides) is a service provider, which this
// architecture does not have (ADR 0001, ADR 0002): an application builds an
// [ImageManager] and keeps it.
//
// usingGd() and usingImagick() name PHP extensions. [Image.Using] takes the
// name a driver was registered under, and the one registered here is
// [StdDriverName].
//
// ensureRequirementsAreMet() asks whether the intervention/image package is
// installed. There is nothing to install and nothing to ask.
//
// __serialize() throws to stop an image being serialized. Go has no serializer
// that would reach into an unexported field, so there is nothing to refuse.
package image
