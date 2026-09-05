package publish_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/publish"
)

// TestAnIndentedMarkerSurvivesRepeatedMerges is the idempotence check for the
// shape a formatter produces.
//
// A custom block inside a function is indented, because gofmt indents it. If
// the closing marker's own indentation is captured as part of the body, every
// merge re-emits it and the file grows a tab per publish -- reported as an
// update, forever, on a file nobody edited.
func TestAnIndentedMarkerSurvivesRepeatedMerges(t *testing.T) {
	const generated = "package app\n\nfunc Boot() {\n\t// arandu:begin custom\n\t// arandu:end custom\n}\n"

	edited := strings.Replace(generated,
		"\t// arandu:begin custom\n\t// arandu:end custom",
		"\t// arandu:begin custom\n\tsetup()\n\t// arandu:end custom", 1)

	first := publish.Merge("app/boot.go", []byte(edited), []byte(generated))
	second := publish.Merge("app/boot.go", first, []byte(generated))
	third := publish.Merge("app/boot.go", second, []byte(generated))

	if string(first) != string(second) {
		t.Fatalf("the second merge changed a file the first had settled:\nfirst:\n%q\nsecond:\n%q", first, second)
	}
	if string(second) != string(third) {
		t.Fatalf("the file is still moving on the third merge:\n%q", third)
	}
	if !strings.Contains(string(third), "\tsetup()\n") {
		t.Fatalf("the edit did not survive:\n%q", third)
	}
	if strings.Contains(string(third), "\t\t// arandu:end custom") {
		t.Fatalf("the closing marker gained indentation it did not have:\n%q", third)
	}
}
