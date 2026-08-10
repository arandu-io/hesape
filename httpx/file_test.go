package httpx_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/filesystem"
	"github.com/arandu-io/hesape/httpx"
)

// uploaded builds the request a browser makes when a form has a file on it.
func uploaded(t *testing.T, field, filename, content string) *http.Request {
	t.Helper()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("building the form: %v", err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("closing the form: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/documents", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	return r
}

func TestAFileArrivesWithItsBytesReadableMoreThanOnce(t *testing.T) {
	ctx := httpx.NewContext(httptest.NewRecorder(), uploaded(t, "document", "invoice.pdf", "%PDF-1.7"), nil, nil)

	up, err := httpx.File(ctx, "document")
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if up.Field != "document" || up.Name != "invoice.pdf" {
		t.Errorf("arrived as %+v", up)
	}
	if up.Size != int64(len("%PDF-1.7")) {
		t.Errorf("Size = %d, and the size is the one field the server counts", up.Size)
	}

	// Twice, because a rule is checked and then the bytes are stored.
	for range 2 {
		f, err := up.Open()
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		got, err := io.ReadAll(f)
		if err != nil {
			t.Fatalf("reading: %v", err)
		}
		_ = f.Close()
		if string(got) != "%PDF-1.7" {
			t.Errorf("read %q", got)
		}
	}
}

func TestAFilenameThatReadsAsADirectoryIsNotOne(t *testing.T) {
	// The name is what to call the file if it is ever offered back, never a key.
	// filesystem.FromMultipart is where that is enforced, and this is the path
	// that hands it the header.
	ctx := httpx.NewContext(httptest.NewRecorder(), uploaded(t, "document", `..\..\etc\passwd`, "root:x:0:0"), nil, nil)

	up, err := httpx.File(ctx, "document")
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if strings.ContainsAny(up.Name, `/\`) {
		t.Errorf("Name = %q, which reads as a path", up.Name)
	}
}

func TestAFieldWithNoFileInItSaysWhichFieldItWas(t *testing.T) {
	ctx := httpx.NewContext(httptest.NewRecorder(), uploaded(t, "document", "invoice.pdf", "%PDF-1.7"), nil, nil)

	_, err := httpx.File(ctx, "attachment")
	if err == nil {
		t.Fatal("a field nobody filled produced an upload")
	}
	if !strings.Contains(err.Error(), "attachment") {
		t.Errorf("the failure does not name the field: %v", err)
	}
}

func TestAnUploadIsCheckedAgainstRulesRatherThanTrusted(t *testing.T) {
	// The rules live in hesape/filesystem; what this asserts is that what File
	// returns is the value they take, so a handler has one shape to work with.
	ctx := httpx.NewContext(httptest.NewRecorder(), uploaded(t, "document", "invoice.exe", "MZ"), nil, nil)

	up, err := httpx.File(ctx, "document")
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if err := up.Check(filesystem.UploadRules{MaxBytes: 1 << 20, Extensions: []string{".pdf"}}); err == nil {
		t.Error("an executable was accepted by a rule that lists .pdf")
	}
}
