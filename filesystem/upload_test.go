package filesystem_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"path"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/filesystem"
)

func upload(name string, size int64) filesystem.Upload {
	body := []byte("x")
	var encoded bytes.Buffer
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		_ = png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1)))
		body = encoded.Bytes()
	case ".jpg", ".jpeg":
		_ = jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil)
		body = encoded.Bytes()
	}
	return filesystem.Upload{
		Field: "avatar",
		Name:  name,
		Size:  size,
		Open:  func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil },
	}
}

var pngRules = filesystem.UploadRules{MaxBytes: 1024, Extensions: []string{".png", ".jpg"}}

func TestAnAcceptableUploadPasses(t *testing.T) {
	if err := upload("photo.png", 100).Check(pngRules); err != nil {
		t.Fatalf("Check: %v", err)
	}
	// The extension comparison is case-insensitive on both sides, because a
	// phone sends IMG_0001.JPG.
	if err := upload("IMG_0001.JPG", 100).Check(pngRules); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

// TestUploadRulesUseTheContentExtension: the announced name and type are both
// controlled by the sender and cannot turn text into an accepted image.
func TestUploadRulesUseTheContentExtension(t *testing.T) {
	picture := encodedPNG(t)
	valid := filesystem.Upload{
		Field:       "avatar",
		Name:        "payload.php",
		Size:        int64(len(picture)),
		ContentType: "text/x-php",
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(picture)), nil
		},
	}
	if err := valid.Check(filesystem.UploadRules{MaxBytes: 1024, Extensions: []string{".png"}}); err != nil {
		t.Fatalf("valid PNG with a misleading name was refused: %v", err)
	}

	malicious := filesystem.Upload{
		Field:       "avatar",
		Name:        "avatar.png",
		Size:        19,
		ContentType: "image/png",
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("<?php echo 'owned';")), nil
		},
	}
	if err := malicious.Check(filesystem.UploadRules{MaxBytes: 1024, Extensions: []string{".png"}}); !errors.Is(err, filesystem.ErrRefusedUpload) {
		t.Fatalf("renamed PHP err = %v, want ErrRefusedUpload", err)
	}
}

// TestUploadRulesRefuseAPayloadAppendedToAnImage: a payload riding behind a
// valid PNG header is not a PNG, and classification must not scale its
// allocation or read with the size of input an attacker supplied.
func TestUploadRulesRefuseAPayloadAppendedToAnImage(t *testing.T) {
	picture := append(encodedPNG(t), bytes.Repeat([]byte("x"), 2<<20)...)
	read := 0
	upload := filesystem.Upload{
		Field: "avatar",
		Name:  "avatar.bin",
		Size:  int64(len(picture)),
		Open: func() (io.ReadCloser, error) {
			return &countingReadCloser{Reader: bytes.NewReader(picture), read: &read}, nil
		},
	}

	if err := upload.Check(filesystem.UploadRules{MaxBytes: int64(len(picture)), Extensions: []string{".png"}}); !errors.Is(err, filesystem.ErrRefusedUpload) {
		t.Fatalf("Check err = %v, want ErrRefusedUpload", err)
	}
	if read == 0 {
		t.Fatal("Check answered without inspecting the content")
	}
	// Refusing the tail must not mean reading it: the decoder stops at the end
	// of the image and only the trailing budget is read past it.
	if read > 64<<10 {
		t.Fatalf("Check read %d bytes to classify the upload, want at most 64 KiB", read)
	}
}

// TestPutFileUsesTheContentExtension: the default generated key must not carry
// a suffix chosen by the client after validation accepted different bytes.
func TestPutFileUsesTheContentExtension(t *testing.T) {
	picture := encodedPNG(t)
	upload := filesystem.Upload{
		Field:       "avatar",
		Name:        "payload.php",
		Size:        int64(len(picture)),
		ContentType: "text/x-php",
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(picture)), nil
		},
	}

	key, err := localDisk(t).PutFile(context.Background(), grant(tenant), "avatars", upload)
	if err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	if !strings.HasSuffix(key, ".png") {
		t.Fatalf("key = %q, want the extension detected from the PNG bytes", key)
	}
	if strings.HasSuffix(key, ".php") {
		t.Fatalf("key = %q, and kept the client-announced extension", key)
	}
}

type countingReadCloser struct {
	*bytes.Reader
	read *int
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	*r.read += n
	return n, err
}

func (r *countingReadCloser) Close() error { return nil }

func encodedPNG(t *testing.T) []byte {
	t.Helper()

	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestAnUploadOverTheLimitIsRefused(t *testing.T) {
	err := upload("photo.png", 2048).Check(pngRules)
	if !errors.Is(err, filesystem.ErrRefusedUpload) {
		t.Fatalf("err = %v, want ErrRefusedUpload", err)
	}
	if !strings.Contains(err.Error(), "2048") || !strings.Contains(err.Error(), "1024") {
		t.Fatalf("err = %q, want both sizes in it -- somebody reads this", err)
	}
}

func TestAnUnacceptableExtensionIsRefused(t *testing.T) {
	for _, name := range []string{"payload.exe", "photo.png.exe", "noextension"} {
		if err := upload(name, 100).Check(pngRules); !errors.Is(err, filesystem.ErrRefusedUpload) {
			t.Errorf("%q was accepted", name)
		}
	}
}

func TestAnEmptyUploadIsRefused(t *testing.T) {
	if err := upload("photo.png", 0).Check(pngRules); !errors.Is(err, filesystem.ErrRefusedUpload) {
		t.Fatal("a zero-byte upload was accepted")
	}
	u := upload("photo.png", 10)
	u.Open = nil
	if err := u.Check(pngRules); !errors.Is(err, filesystem.ErrRefusedUpload) {
		t.Fatal("an upload with no content was accepted")
	}
}

// TestRulesFailClosed is the decision worth a test of its own. A rules value
// that named no maximum would accept a four gigabyte file, and one that named
// no extension would accept an .exe -- and the moment those are the defaults,
// the rule that was forgotten looks exactly like the rule that was written.
func TestRulesFailClosed(t *testing.T) {
	if err := upload("photo.png", 10).Check(filesystem.UploadRules{Extensions: []string{".png"}}); err == nil {
		t.Error("rules with no MaxBytes accepted an upload")
	}
	if err := upload("photo.png", 10).Check(filesystem.UploadRules{MaxBytes: 1024}); err == nil {
		t.Error("rules with no Extensions accepted an upload")
	}
	if err := upload("photo.png", 10).Check(filesystem.UploadRules{}); err == nil {
		t.Error("the zero UploadRules accepted an upload")
	}
}

// TestTheAnnouncedNameIsNeverADirectory: the filename is a string the client
// chose, and a browser is not the only thing that posts a form.
func TestTheAnnouncedNameIsNeverADirectory(t *testing.T) {
	for _, sent := range []string{
		"../../etc/passwd",
		`C:\Users\admin\secret.png`,
		"/etc/passwd",
		"a/b/photo.png",
	} {
		u := fromForm(t, "avatar", sent, "body")
		if strings.ContainsAny(u.Name, `/\`) {
			t.Errorf("filename %q survived as %q", sent, u.Name)
		}
	}
}

func TestFromMultipartCarriesTheFile(t *testing.T) {
	u := fromForm(t, "avatar", "photo.png", "the bytes")

	if u.Field != "avatar" {
		t.Fatalf("Field = %q", u.Field)
	}
	if u.Name != "photo.png" {
		t.Fatalf("Name = %q", u.Name)
	}
	if u.Size != int64(len("the bytes")) {
		t.Fatalf("Size = %d", u.Size)
	}
	body, err := u.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "the bytes" {
		t.Fatalf("body = %q", got)
	}
	// Twice, because a handler checks the rules and then stores it.
	again, err := u.Open()
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	again.Close()
}

// fromForm builds an Upload the way a request does: through a real multipart
// body, so the parsing is the one that runs in production.
func fromForm(t *testing.T, field, filename, body string) filesystem.Upload {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r := multipart.NewReader(&buf, w.Boundary())
	form, err := r.ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })

	headers := form.File[field]
	if len(headers) != 1 {
		t.Fatalf("got %d parts for %q", len(headers), field)
	}
	return filesystem.FromMultipart(field, headers[0])
}
