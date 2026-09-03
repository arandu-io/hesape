package mail_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/mail"
)

type unclassifiedUpload struct{}

func (unclassifiedUpload) GetClientOriginalName() string { return "payload.png" }
func (unclassifiedUpload) GetMimeType() string           { return "" }
func (unclassifiedUpload) GetClientMimeType() string     { return "image/png" }
func (unclassifiedUpload) Get() ([]byte, error)          { return []byte("plain text"), nil }

// TestAnUnclassifiedUploadDoesNotFallBackToClientMime: a detection failure is
// not permission to trust the header that necessitated detection.
func TestAnUnclassifiedUploadDoesNotFallBackToClientMime(t *testing.T) {
	message := &mail.Message{}
	if err := mail.FromUploadedFile(unclassifiedUpload{}).AttachTo(message); err != nil {
		t.Fatal(err)
	}
	if len(message.RawAttachments) != 1 {
		t.Fatalf("RawAttachments = %d, want 1", len(message.RawAttachments))
	}
	if got := message.RawAttachments[0].Options.Mime; got != "application/octet-stream" {
		t.Fatalf("attachment MIME = %q, want fail-closed application/octet-stream", got)
	}

	rendered := mustRender(t, *message)
	if !strings.Contains(rendered, `Content-Type: application/octet-stream; name="payload.png"`) {
		t.Fatalf("rendered attachment did not keep the fail-closed MIME:\n%s", rendered)
	}
	if strings.Contains(rendered, `Content-Type: image/png; name="payload.png"`) {
		t.Fatal("rendered attachment re-inferred image/png from the client filename")
	}
}
