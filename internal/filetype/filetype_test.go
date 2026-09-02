package filetype_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"testing"

	"github.com/arandu-io/hesape/internal/filetype"
)

func TestDetectRejectsIncompleteDecodedImages(t *testing.T) {
	pngBody := encodePNG(t)
	jpegBody := encodeJPEG(t)
	gifBody := encodeGIF(t)

	scan := jpegScanOffset(t, jpegBody)

	for _, test := range []struct {
		name      string
		body      []byte
		mediaType string
		extension string
	}{
		{name: "PNG without IDAT or IEND", body: pngBody[:33], mediaType: "image/png", extension: "png"},
		{name: "JPEG without scan or EOI", body: jpegBody[:scan], mediaType: "image/jpeg", extension: "jpeg"},
		{name: "GIF logical screen only", body: gifBody[:10], mediaType: "image/gif", extension: "gif"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mediaType, extension, _ := filetype.Detect(openBytes(test.body))
			if mediaType == test.mediaType || extension == test.extension {
				t.Fatalf("Detect = (%q, %q), accepted an incomplete encoded image", mediaType, extension)
			}
			if _, _, ok := filetype.Image(openBytes(test.body), false); ok {
				t.Fatal("Image accepted an incomplete encoded image")
			}
		})
	}
}

func TestDetectRejectsGIFWithATruncatedTrailingExtension(t *testing.T) {
	body := encodeGIF(t)
	body = append(body[:len(body)-1], 0x21, 0xfe, 0x01)

	mediaType, extension, _ := filetype.Detect(openBytes(body))
	if mediaType == "image/gif" || extension == "gif" {
		t.Fatalf("Detect = (%q, %q), accepted a GIF with a truncated trailing extension", mediaType, extension)
	}
	if _, _, ok := filetype.Image(openBytes(body), false); ok {
		t.Fatal("Image accepted a GIF with a truncated trailing extension")
	}
}

func TestDetectAcceptsJPEGWithMetadataBeyondTheSniffPrefix(t *testing.T) {
	body := encodeJPEG(t)
	metadata := bytes.Repeat([]byte{'x'}, 4094)
	segment := []byte{0xff, 0xe1, 0x10, 0x00}
	segment = append(segment, metadata...)
	body = append(append(append([]byte{}, body[:2]...), segment...), body[2:]...)

	mediaType, extension, ok := filetype.Detect(openBytes(body))
	if !ok || mediaType != "image/jpeg" || extension != "jpeg" {
		t.Fatalf("Detect = (%q, %q, %v), want a valid JPEG beyond 512 bytes", mediaType, extension, ok)
	}
	if width, height, ok := filetype.Image(openBytes(body), false); !ok || width != 2 || height != 2 {
		t.Fatalf("Image = (%d, %d, %v), want (2, 2, true)", width, height, ok)
	}
}

func TestImageAcceptsOnlyFullyDecodedStandardLibraryFormats(t *testing.T) {
	for _, test := range []struct {
		name string
		body []byte
	}{
		{name: "PNG", body: encodePNG(t)},
		{name: "JPEG", body: encodeJPEG(t)},
		{name: "GIF", body: encodeGIF(t)},
	} {
		t.Run(test.name, func(t *testing.T) {
			width, height, ok := filetype.Image(openBytes(test.body), false)
			if !ok || width != 2 || height != 2 {
				t.Fatalf("Image = (%d, %d, %v), want (2, 2, true)", width, height, ok)
			}
		})
	}
}

func TestBMPAndWebPAreClassifiedButNotAcceptedAsDecodedImages(t *testing.T) {
	webp := decodeBase64(t, "UklGRk4AAABXRUJQVlA4WAoAAAAQAAAAAAAAAAAAQUxQSAIAAAAATVZQOCAmAAAAcAEAnQEqAQABAAIANCWQAnQBQAAA/t4Sfuj6y++oYtWWohvwAAA=")
	for _, test := range []struct {
		name      string
		body      []byte
		mediaType string
		extension string
	}{
		{name: "BMP", body: validBMP(), mediaType: "image/bmp", extension: "bmp"},
		{name: "WebP", body: webp, mediaType: "image/webp", extension: "webp"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mediaType, extension, ok := filetype.Detect(openBytes(test.body))
			if !ok || mediaType != test.mediaType || extension != test.extension {
				t.Fatalf("Detect = (%q, %q, %v)", mediaType, extension, ok)
			}
			if _, _, ok := filetype.Image(openBytes(test.body), false); ok {
				t.Fatal("Image accepted a format without an audited decoder")
			}
		})
	}
}

func TestDetectRejectsFabricatedBMPAndWebPContainers(t *testing.T) {
	webp := decodeBase64(t, "UklGRk4AAABXRUJQVlA4WAoAAAAQAAAAAAAAAAAAQUxQSAIAAAAATVZQOCAmAAAAcAEAnQEqAQABAAIANCWQAnQBQAAA/t4Sfuj6y++oYtWWohvwAAA=")
	fakeBMP := make([]byte, 58)
	copy(fakeBMP, "BM")
	binary.LittleEndian.PutUint32(fakeBMP[2:6], uint32(len(fakeBMP)))
	binary.LittleEndian.PutUint32(fakeBMP[10:14], 54)
	binary.LittleEndian.PutUint32(fakeBMP[14:18], 40)
	binary.LittleEndian.PutUint32(fakeBMP[18:22], 1)
	binary.LittleEndian.PutUint32(fakeBMP[22:26], 1)

	fakeWebP := make([]byte, 30)
	copy(fakeWebP, "RIFF")
	binary.LittleEndian.PutUint32(fakeWebP[4:8], 22)
	copy(fakeWebP[8:16], "WEBPVP8 ")
	binary.LittleEndian.PutUint32(fakeWebP[16:20], 10)
	copy(fakeWebP[23:26], []byte{0x9d, 0x01, 0x2a})
	binary.LittleEndian.PutUint16(fakeWebP[26:28], 1)
	binary.LittleEndian.PutUint16(fakeWebP[28:30], 1)

	for _, test := range []struct {
		name      string
		body      []byte
		mediaType string
	}{
		{name: "BMP with no pixel declaration", body: fakeBMP, mediaType: "image/bmp"},
		{name: "truncated BMP", body: validBMP()[:54], mediaType: "image/bmp"},
		{name: "WebP with no coded frame", body: fakeWebP, mediaType: "image/webp"},
		{name: "truncated WebP", body: webp[:len(webp)-1], mediaType: "image/webp"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mediaType, _, _ := filetype.Detect(openBytes(test.body))
			if mediaType == test.mediaType {
				t.Fatalf("Detect returned %q for a fabricated container", mediaType)
			}
		})
	}
}

func TestSVGNeedsACompleteDocumentAndExplicitImagePermission(t *testing.T) {
	valid := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0h1v1z"/></svg>`)
	mediaType, extension, ok := filetype.Detect(openBytes(valid))
	if !ok || mediaType != "image/svg+xml" || extension != "svg" {
		t.Fatalf("Detect = (%q, %q, %v)", mediaType, extension, ok)
	}
	if _, _, ok := filetype.Image(openBytes(valid), false); ok {
		t.Fatal("SVG passed without explicit permission")
	}
	if _, _, ok := filetype.Image(openBytes(valid), true); !ok {
		t.Fatal("well-formed SVG failed with explicit permission")
	}

	malformed := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><g></svg>`)
	if mediaType, extension, _ := filetype.Detect(openBytes(malformed)); mediaType == "image/svg+xml" || extension == "svg" {
		t.Fatalf("Detect = (%q, %q), accepted malformed SVG", mediaType, extension)
	}
}

func TestDetectReturnsStableExtensionsForSnifferTypes(t *testing.T) {
	for _, test := range []struct {
		name      string
		body      []byte
		mediaType string
		extension string
	}{
		{name: "PDF", body: []byte("%PDF-1.7\n%%EOF"), mediaType: "application/pdf", extension: "pdf"},
		{name: "PostScript", body: []byte("%!PS-Adobe-3.0\n"), mediaType: "application/postscript", extension: "ps"},
		{name: "icon", body: []byte{0, 0, 1, 0, 1, 0, 0, 0}, mediaType: "image/x-icon", extension: "ico"},
		{name: "gzip", body: []byte{0x1f, 0x8b, 0x08, 0}, mediaType: "application/x-gzip", extension: "gz"},
		{name: "zip", body: []byte{'P', 'K', 3, 4, 0, 0}, mediaType: "application/zip", extension: "zip"},
		{name: "RAR", body: []byte("Rar!\x1a\x07\x00payload"), mediaType: "application/x-rar-compressed", extension: "rar"},
		{name: "WebAssembly", body: []byte{0, 'a', 's', 'm', 1, 0, 0, 0}, mediaType: "application/wasm", extension: "wasm"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mediaType, extension, ok := filetype.Detect(openBytes(test.body))
			if !ok || mediaType != test.mediaType || extension != test.extension {
				t.Fatalf("Detect = (%q, %q, %v), want (%q, %q, true)", mediaType, extension, ok, test.mediaType, test.extension)
			}
		})
	}
}

func TestDetectReadsABoundedAmountFromUnclassifiedContent(t *testing.T) {
	read := 0
	body := bytes.Repeat([]byte{0}, 2<<20)
	open := func() (io.ReadCloser, error) {
		return &countingReader{Reader: bytes.NewReader(body), read: &read}, nil
	}
	filetype.Detect(open)
	if read == 0 || read > 512 {
		t.Fatalf("Detect read %d bytes, want 1..512", read)
	}
}

func TestMalformedClassifiedImageStopsAtTheEncodedBodyLimit(t *testing.T) {
	// DecodeConfig can finish from this valid 1x1 GIF screen descriptor. The
	// full decoder then sees an endless sequence of well-formed comment blocks
	// but never an image frame, forcing the encoded-body limiter to stop it.
	header := []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff")
	comment := append([]byte{0x21, 0xfe, 0xff}, make([]byte, 255)...)
	comment = append(comment, 0)

	var reads []*int64
	open := func() (io.ReadCloser, error) {
		count := new(int64)
		reads = append(reads, count)
		return &trackedReadCloser{
			Reader: io.MultiReader(
				bytes.NewReader(header),
				&repeatingReader{pattern: comment},
			),
			read: count,
		}, nil
	}

	mediaType, extension, ok := filetype.Detect(open)
	if !ok || mediaType != "application/octet-stream" || extension != "bin" {
		t.Fatalf("Detect = (%q, %q, %v), want a fail-closed classification", mediaType, extension, ok)
	}
	var maximum int64
	for _, read := range reads {
		if *read > maximum {
			maximum = *read
		}
	}
	if maximum < 63<<20 {
		t.Fatalf("largest read was %d bytes; malformed source did not exercise the body limit", maximum)
	}
	if maximum > 64<<20 {
		t.Fatalf("largest read was %d bytes, exceeded the 64 MiB body limit", maximum)
	}
}

func TestDetectFailsClosedForEmptyOrUnreadableContent(t *testing.T) {
	if mediaType, extension, ok := filetype.Detect(openBytes(nil)); ok || mediaType != "" || extension != "" {
		t.Fatalf("empty Detect = (%q, %q, %v)", mediaType, extension, ok)
	}
	if mediaType, extension, ok := filetype.Detect(func() (io.ReadCloser, error) { return nil, io.ErrUnexpectedEOF }); ok || mediaType != "" || extension != "" {
		t.Fatalf("unreadable Detect = (%q, %q, %v)", mediaType, extension, ok)
	}
}

func openBytes(body []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
}

type countingReader struct {
	*bytes.Reader
	read *int
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	*r.read += n
	return n, err
}

func (r *countingReader) Close() error { return nil }

type trackedReadCloser struct {
	io.Reader
	read *int64
}

func (r *trackedReadCloser) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	*r.read += int64(n)
	return n, err
}

func (r *trackedReadCloser) Close() error { return nil }

type repeatingReader struct {
	pattern []byte
	offset  int
}

func (r *repeatingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.pattern[r.offset]
		r.offset++
		if r.offset == len(r.pattern) {
			r.offset = 0
		}
	}
	return len(p), nil
}

func encodePNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func encodeJPEG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := jpeg.Encode(&out, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func encodeGIF(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := gif.Encode(&out, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func jpegScanOffset(t *testing.T, body []byte) int {
	t.Helper()
	if len(body) < 2 || body[0] != 0xff || body[1] != 0xd8 {
		t.Fatal("encoded JPEG has no SOI marker")
	}
	for offset := 2; offset < len(body); {
		start := offset
		for offset < len(body) && body[offset] == 0xff {
			offset++
		}
		if offset >= len(body) {
			break
		}
		marker := body[offset]
		offset++
		if marker == 0xda {
			return start
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd9 {
			continue
		}
		if offset+2 > len(body) {
			break
		}
		length := int(binary.BigEndian.Uint16(body[offset : offset+2]))
		if length < 2 || offset+length > len(body) {
			break
		}
		offset += length
	}
	t.Fatal("encoded JPEG has no scan marker")
	return 0
}

func validBMP() []byte {
	body := make([]byte, 58)
	copy(body, "BM")
	binary.LittleEndian.PutUint32(body[2:6], uint32(len(body)))
	binary.LittleEndian.PutUint32(body[10:14], 54)
	binary.LittleEndian.PutUint32(body[14:18], 40)
	binary.LittleEndian.PutUint32(body[18:22], 1)
	binary.LittleEndian.PutUint32(body[22:26], 1)
	binary.LittleEndian.PutUint16(body[26:28], 1)
	binary.LittleEndian.PutUint16(body[28:30], 24)
	body[54] = 0x7f
	return body
}

func decodeBase64(t *testing.T, encoded string) []byte {
	t.Helper()
	body, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
