package routing_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/arandu-io/hesape/routing"
)

// exporter is a single-action controller.
type exporter struct{ ran *bool }

func (c exporter) Invoke(*request) error { *c.ran = true; return nil }

func TestAnInvokableControllerAnswersItsOneRoute(t *testing.T) {
	r := routing.NewRouter()
	ran := false
	route := routing.Invokable(r, "post", "/exports", exporter{ran: &ran}, adapt).Name("exports.store")

	if route.Method != http.MethodPost || route.Pattern != "/exports" {
		t.Errorf("registered %s %s, want POST /exports", route.Method, route.Pattern)
	}
	if path, err := r.Table().Route("exports.store"); err != nil || path != "/exports" {
		t.Errorf("Route(exports.store) = %q, %v; want /exports", path, err)
	}

	if code := answer(r, http.MethodPost, "/exports"); code != http.StatusOK || !ran {
		t.Fatalf("POST /exports = %d, ran %v; want 200 and the controller invoked", code, ran)
	}
	if code := answer(r, http.MethodGet, "/exports"); code != http.StatusMethodNotAllowed {
		t.Errorf("GET /exports = %d, want 405: the route answers the one method it was given", code)
	}
}

// failingExport returns an error, which only the adapter may turn into an
// answer.
type failingExport struct{}

func (failingExport) Invoke(*request) error { return errors.New("the disk is full") }

func TestWhatAnInvokableReturnsReachesTheAdapter(t *testing.T) {
	r := routing.NewRouter()
	routing.Invokable(r, http.MethodPost, "/exports", failingExport{}, adapt)

	if code := answer(r, http.MethodPost, "/exports"); code != http.StatusInternalServerError {
		t.Fatalf("POST /exports = %d, want 500: the error did not reach the adapter", code)
	}
}

func TestInvokableWithoutAMethodPanics(t *testing.T) {
	mustPanicWith(t, func() {
		routing.Invokable(routing.NewRouter(), "", "/exports", exporter{ran: new(bool)}, adapt)
	}, "no method")
}
