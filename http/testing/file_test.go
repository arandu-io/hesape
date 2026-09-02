package testing_test

import (
	"testing"

	httptesting "github.com/arandu-io/hesape/http/testing"
)

// TestMimeTypeSetsOnlyClientMetadata: a fake must exercise the same trust
// boundary as a browser upload instead of overriding server classification.
func TestMimeTypeSetsOnlyClientMetadata(t *testing.T) {
	file, err := httptesting.CreateWithContent("avatar.bin", "plain text")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	file.MimeType("image/png")

	if got := file.GetClientMimeType(); got != "image/png" {
		t.Fatalf("GetClientMimeType() = %q, want image/png", got)
	}
	if got := file.GetMimeType(); got == "image/png" {
		t.Fatal("the fake let client metadata override content classification")
	}
}
