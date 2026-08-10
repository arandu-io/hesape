package filesystem_test

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/filesystem"
)

func upload(name string, size int64) filesystem.Upload {
	return filesystem.Upload{
		Field: "avatar",
		Name:  name,
		Size:  size,
		Open:  func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("x")), nil },
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
