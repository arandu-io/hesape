package foundation_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/log"
)

// TestACollectedLineCarriesTheRequestFields holds that a line written inside a
// request reaches the Collector with the request's id, method and path, as it
// reaches stdout. A line in the debug console that does not say which request
// wrote it is a line nobody can place.
func TestACollectedLineCarriesTheRequestFields(t *testing.T) {
	rec := log.NewRecorder(4)
	h := foundation.Observe(true, "", rec)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.For(r.Context()).Info("inside")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/p", nil))
	got := rec.Recent(1)
	if len(got) != 1 {
		t.Fatal("nothing recorded")
	}
	for _, l := range got[0].Collector.Logs() {
		if l.Message == "inside" {
			if l.Attrs["request_id"] != got[0].RequestID || l.Attrs["method"] != "GET" || l.Attrs["path"] != "/p" {
				t.Fatalf("collected line attrs = %v", l.Attrs)
			}
			return
		}
	}
	t.Fatal("the line was not collected")
}

// TestACollectedLineCarriesTheRequestFieldsUnderTheRootLogger is the same
// promise with the application's logger already in the context, which is how
// a served application runs.
func TestACollectedLineCarriesTheRequestFieldsUnderTheRootLogger(t *testing.T) {
	rec := log.NewRecorder(4)
	h := log.Middleware(log.New("prod", log.LevelInfo))(foundation.Observe(true, "", rec)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.For(r.Context()).Info("inside")
	})))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/p", nil))
	got := rec.Recent(1)
	if len(got) != 1 {
		t.Fatal("nothing recorded")
	}
	for _, l := range got[0].Collector.Logs() {
		if l.Message == "inside" {
			if l.Attrs["request_id"] != got[0].RequestID || l.Attrs["method"] != "GET" || l.Attrs["path"] != "/p" {
				t.Fatalf("collected line attrs = %v", l.Attrs)
			}
			return
		}
	}
	t.Fatal("the line was not collected")
}
