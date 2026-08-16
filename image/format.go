package image

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
)

// outputFormats are the names [Image.ToFormat] accepts, in order. Accepting a
// name here is not a promise that this driver can write it -- see
// [encodeCanvas], which says so at the point where it would have to.
var outputFormats = []string{"webp", "jpg", "jpeg", "png", "gif", "avif", "heic", "heif", "bmp"}

// inputTypes are the media types this driver agrees to open.
var inputTypes = []string{
	"image/jpeg", "image/png", "image/bmp", "image/gif", "image/webp",
	"image/avif", "image/x-avif", "image/heic", "image/x-heic", "image/heif",
}

// sniffMimeType returns the media type read out of the bytes themselves,
// never out of a filename or a header somebody sent.
//
// It knows the types [Image.Extension] can name, and returns
// "application/octet-stream" for anything else, which is what makes
// Extension's last case, "bin", reachable.
func sniffMimeType(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "image/jpeg"
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return "image/gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	case len(b) >= 2 && b[0] == 'B' && b[1] == 'M':
		return "image/bmp"
	case len(b) >= 4 && (string(b[:4]) == "II*\x00" || string(b[:4]) == "MM\x00*"):
		return "image/tiff"
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		switch string(b[8:12]) {
		case "avif", "avis":
			return "image/avif"
		case "heic", "heix", "heim", "heis", "hevc", "hevx", "hevm", "hevs", "mif1", "msf1":
			return "image/heic"
		}
	}
	if head := strings.TrimLeft(string(firstBytes(b, 256)), " \t\r\n"); strings.HasPrefix(head, "<svg") || strings.HasPrefix(head, "<?xml") {
		return "image/svg+xml"
	}
	return "application/octet-stream"
}

func firstBytes(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}

// supportedInput reports whether this driver will open this media type at
// all.
func supportedInput(mediaType string) bool {
	for _, t := range inputTypes {
		if t == mediaType {
			return true
		}
	}
	return false
}

// decodeCanvas opens the bytes onto the working canvas and says which format
// they were in, so that an image nobody asked to convert is written back as
// what it was.
func decodeCanvas(b []byte) (*stdimage.RGBA, string, error) {
	src, format, err := stdimage.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, "", failWith(err, "the image could not be decoded (this driver reads jpeg, png and gif; %s was detected)", sniffMimeType(b))
	}
	return toRGBA(src), format, nil
}

// encodeCanvas writes the canvas back out.
//
// Three formats can be written, and the other five accepted names fail here
// with a message that says so. That is deliberate: WebP, AVIF, HEIC and BMP
// have no encoder in the standard library, and the packages that do have one
// are dependencies this module does not carry. Failing at the encode with the
// format named is the honest version of that -- the alternative is
// [Image.ToWebp] quietly handing back a JPEG, which is a lie that reaches
// production as a broken Content-Type.
func encodeCanvas(canvas *stdimage.RGBA, format string, quality int) ([]byte, error) {
	var buf bytes.Buffer
	switch format {
	case "jpg", "jpeg":
		// JPEG has no alpha channel. Compositing over white first is what
		// every other encoder does with transparency, and skipping it paints
		// every transparent pixel black.
		opaque := flatten(canvas, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		if err := jpeg.Encode(&buf, opaque, &jpeg.Options{Quality: quality}); err != nil {
			return nil, failWith(err, "encoding jpeg")
		}
	case "png":
		// No quality is passed to the PNG encoder: the format is lossless and
		// the number would mean compression effort, not fidelity.
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		if err := enc.Encode(&buf, canvas); err != nil {
			return nil, failWith(err, "encoding png")
		}
	case "gif":
		if err := gif.Encode(&buf, canvas, nil); err != nil {
			return nil, failWith(err, "encoding gif")
		}
	case "webp", "avif", "heic", "heif", "bmp":
		return nil, fail("the [%s] format cannot be written: this driver writes jpeg, png and gif", format)
	default:
		return nil, fail("the [%s] format is not supported", format)
	}
	return buf.Bytes(), nil
}

// extensionFor maps a media type to a file extension, kept in one place
// because [Image.HashName] and [Image.Extension] both need it.
func extensionFor(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/avif", "image/x-avif":
		return "avif"
	case "image/heic", "image/x-heic", "image/heif":
		return "heic"
	case "image/bmp":
		return "bmp"
	case "image/svg+xml":
		return "svg"
	case "image/tiff":
		return "tiff"
	default:
		return "bin"
	}
}
