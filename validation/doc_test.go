package validation_test

import (
	"os"
	"reflect"
	"regexp"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
)

// TestThePackageDocCallsOnlyWhatAContextHas: the doc showed ctx.Validate
// after the method was removed, and a reader copying the example met a
// compile error on the first line.
func TestThePackageDocCallsOnlyWhatAContextHas(t *testing.T) {
	doc, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatal(err)
	}
	context := reflect.TypeOf(&hhttp.Context{})
	for _, call := range regexp.MustCompile(`\bctx\.(\w+)\(`).FindAllStringSubmatch(string(doc), -1) {
		if _, ok := context.MethodByName(call[1]); !ok {
			t.Errorf("the doc calls ctx.%s, which *http.Context does not have", call[1])
		}
	}
	if _, ok := reflect.TypeOf(&hhttp.Request{}).MethodByName("Validate"); !ok {
		t.Error("the doc calls Request.Validate, which *http.Request does not have")
	}
}
