package http_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	stdhttp "net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	http "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/validation"
)

// TestUploadedFileMetadataComesFromTheBytes: a filename and Content-Type are
// both strings the client chose, so neither can define the server-side type.
func TestUploadedFileMetadataComesFromTheBytes(t *testing.T) {
	picture := pngBytes(t)
	file := http.NewUploadedFile(multipartHeader(t, "payload.php", "text/x-php", picture), "avatar")

	if got := file.GetMimeType(); got != "image/png" {
		t.Fatalf("GetMimeType() = %q, want image/png from the bytes", got)
	}
	if got := file.GuessExtension(); got != "png" {
		t.Fatalf("GuessExtension() = %q, want png from the bytes", got)
	}
	if got := file.GetClientMimeType(); got != "text/x-php" {
		t.Fatalf("GetClientMimeType() = %q, want the announced metadata kept separately", got)
	}
	if got := file.GetClientOriginalExtension(); got != "php" {
		t.Fatalf("GetClientOriginalExtension() = %q, want php", got)
	}
}

func TestUploadedFileDoesNotTrustAnImageNameOrContentType(t *testing.T) {
	file := http.NewUploadedFile(multipartHeader(t, "avatar.png", "image/png", []byte("<?php echo 'owned';")), "avatar")

	if got := file.GetMimeType(); got == "image/png" {
		t.Fatal("GetMimeType trusted the client-announced image type")
	}
	if got := file.GuessExtension(); got == "png" {
		t.Fatal("GuessExtension trusted the client-announced image name")
	}
}

func TestContentInspectionDoesNotConsumeTheUpload(t *testing.T) {
	picture := pngBytes(t)
	file := http.NewUploadedFile(multipartHeader(t, "avatar.bin", "application/octet-stream", picture), "avatar")

	_ = file.GetMimeType()
	_ = file.GuessExtension()

	for attempt := 1; attempt <= 2; attempt++ {
		body, err := file.Open()
		if err != nil {
			t.Fatalf("Open attempt %d: %v", attempt, err)
		}
		got, err := io.ReadAll(body)
		_ = body.Close()
		if err != nil {
			t.Fatalf("read attempt %d: %v", attempt, err)
		}
		if !bytes.Equal(got, picture) {
			t.Fatalf("read attempt %d returned %d bytes, want the original %d", attempt, len(got), len(picture))
		}
	}
}

func TestRequestValidationAcceptsARealMultipartImageByItsBytes(t *testing.T) {
	req := multipartRequest(t, "avatar", "report.txt", "text/plain", pngBytes(t))
	rules := validation.MustCompile(validation.Rules{
		"avatar": "required|file|image|mimes:png|mimetypes:image/png",
	})

	if _, err := http.NewRequest(req).Validate(rules); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestContentRulesAcceptAValidImageWithAPhpFilename(t *testing.T) {
	for _, rule := range []string{"image", "mimes:png", "mimetypes:image/png"} {
		t.Run(rule, func(t *testing.T) {
			req := multipartRequest(t, "avatar", "payload.php", "text/x-php", pngBytes(t))
			rules := validation.MustCompile(validation.Rules{"avatar": rule})
			if _, err := http.NewRequest(req).Validate(rules); err != nil {
				t.Fatalf("valid PNG with misleading client metadata failed %s: %v", rule, err)
			}
		})
	}
}

func TestRequestValidationRefusesRenamedPHPWithAFalseImageType(t *testing.T) {
	for _, rule := range []string{"image", "mimes:png", "mimetypes:image/png"} {
		t.Run(rule, func(t *testing.T) {
			req := multipartRequest(t, "avatar", "avatar.png", "image/png", []byte("<?php echo 'owned';"))
			rules := validation.MustCompile(validation.Rules{"avatar": rule})
			if _, err := http.NewRequest(req).Validate(rules); err == nil {
				t.Fatalf("renamed PHP with a false image Content-Type passed %s", rule)
			}
		})
	}
}

func TestRequestValidationWrapsEveryMultipartFileInAList(t *testing.T) {
	rules := validation.MustCompile(validation.Rules{
		"files":   "required|array",
		"files.*": "required|file|image",
	})
	valid := []multipartPart{
		{field: "files", filename: "first.txt", contentType: "text/plain", body: pngBytes(t)},
		{field: "files", filename: "second.bin", contentType: "application/octet-stream", body: gifBytes(t)},
	}
	if _, err := http.NewRequest(multipartRequestWithParts(t, valid)).Validate(rules); err != nil {
		t.Fatalf("two valid files failed files.* validation: %v", err)
	}

	spoofed := append([]multipartPart(nil), valid...)
	spoofed[1] = multipartPart{
		field: "files", filename: "second.gif", contentType: "image/gif", body: []byte("<?php echo 'owned';"),
	}
	if _, err := http.NewRequest(multipartRequestWithParts(t, spoofed)).Validate(rules); err == nil {
		t.Fatal("files.* accepted a spoofed second multipart file")
	}
}

func TestImageValidationAcceptsSupportedRasterFormats(t *testing.T) {
	for _, test := range []struct {
		name string
		body []byte
	}{
		{name: "png", body: pngBytes(t)},
		{name: "jpeg", body: jpegBytes(t)},
		{name: "gif", body: gifBytes(t)},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := multipartRequest(t, "avatar", "untrusted.bin", "application/octet-stream", test.body)
			rules := validation.MustCompile(validation.Rules{"avatar": "image"})
			if _, err := http.NewRequest(req).Validate(rules); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

func TestContentRulesRefuseIncompleteRasterBodies(t *testing.T) {
	pngBody := pngBytes(t)
	jpegBody := jpegBytes(t)
	gifBody := gifBytes(t)
	scan := jpegScanOffset(t, jpegBody)

	for _, test := range []struct {
		name      string
		body      []byte
		extension string
		mimeType  string
	}{
		{name: "PNG without IDAT or IEND", body: pngBody[:33], extension: "png", mimeType: "image/png"},
		{name: "JPEG without scan or EOI", body: jpegBody[:scan], extension: "jpeg", mimeType: "image/jpeg"},
		{name: "GIF logical screen only", body: gifBody[:10], extension: "gif", mimeType: "image/gif"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, rule := range []string{"image", "mimes:" + test.extension, "mimetypes:" + test.mimeType} {
				t.Run(rule, func(t *testing.T) {
					req := multipartRequest(t, "avatar", "untrusted.bin", "application/octet-stream", test.body)
					rules := validation.MustCompile(validation.Rules{"avatar": rule})
					if _, err := http.NewRequest(req).Validate(rules); err == nil {
						t.Fatalf("truncated image header passed %s", rule)
					}
				})
			}
		})
	}
}

func TestWebPIsClassifiedButFailsTheImageRuleWithoutAnAuditedDecoder(t *testing.T) {
	body := webPBytes(t)
	file := http.NewUploadedFile(multipartHeader(t, "avatar.bin", "application/octet-stream", body), "avatar")
	if got := file.GetMimeType(); got != "image/webp" {
		t.Fatalf("GetMimeType() = %q, want image/webp", got)
	}
	if got := file.GuessExtension(); got != "webp" {
		t.Fatalf("GuessExtension() = %q, want webp", got)
	}

	rules := validation.MustCompile(validation.Rules{"avatar": "image"})
	if _, err := http.NewRequest(multipartRequest(t, "avatar", "avatar.webp", "image/webp", body)).Validate(rules); err == nil {
		t.Fatal("WebP passed image without an audited decoder")
	}
}

func TestSVGRequiresExplicitPermissionAndWellFormedContent(t *testing.T) {
	valid := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><path d="M0 0h1v1z"/></svg>`)
	defaultRules := validation.MustCompile(validation.Rules{"avatar": "image"})
	if _, err := http.NewRequest(multipartRequest(t, "avatar", "avatar.png", "image/png", valid)).Validate(defaultRules); err == nil {
		t.Fatal("SVG passed the image rule without allow_svg")
	}

	allowedRules := validation.MustCompile(validation.Rules{
		"avatar": "image:allow_svg|mimes:svg|mimetypes:image/svg+xml",
	})
	if _, err := http.NewRequest(multipartRequest(t, "avatar", "avatar.txt", "text/plain", valid)).Validate(allowedRules); err != nil {
		t.Fatalf("explicitly allowed SVG was refused: %v", err)
	}

	malformed := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><g></svg>`)
	if _, err := http.NewRequest(multipartRequest(t, "avatar", "avatar.svg", "image/svg+xml", malformed)).Validate(allowedRules); err == nil {
		t.Fatal("malformed SVG passed image:allow_svg")
	}
}

func multipartRequest(t *testing.T, field, filename, contentType string, content []byte) *stdhttp.Request {
	t.Helper()
	return multipartRequestWithParts(t, []multipartPart{{
		field: field, filename: filename, contentType: contentType, body: content,
	}})
}

type multipartPart struct {
	field       string
	filename    string
	contentType string
	body        []byte
}

func multipartRequestWithParts(t *testing.T, parts []multipartPart) *stdhttp.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range parts {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", `form-data; name="`+file.field+`"; filename="`+file.filename+`"`)
		header.Set("Content-Type", file.contentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(stdhttp.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func multipartHeader(t *testing.T, filename, contentType string, content []byte) *multipart.FileHeader {
	t.Helper()

	req := multipartRequest(t, "avatar", filename, contentType, content)
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = req.MultipartForm.RemoveAll() })
	return req.MultipartForm.File["avatar"][0]
}

func pngBytes(t *testing.T) []byte {
	t.Helper()

	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	canvas.Set(0, 0, color.Black)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func gifBytes(t *testing.T) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := gif.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func webPBytes(t *testing.T) []byte {
	t.Helper()
	encoded, err := base64.StdEncoding.DecodeString("UklGRk4AAABXRUJQVlA4WAoAAAAQAAAAAAAAAAAAQUxQSAIAAAAATVZQOCAmAAAAcAEAnQEqAQABAAIANCWQAnQBQAAA/t4Sfuj6y++oYtWWohvwAAA=")
	if err != nil {
		t.Fatal(err)
	}
	return encoded
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
