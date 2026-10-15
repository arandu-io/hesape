package http_test

import (
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
)

// TestAPageIsAnsweredWithTheStatusItSays: View answers 200, so a page that
// said the record was gone or had changed answered 200 too -- or went out as
// a Fragment of a whole page, or was written by hand -- and the logs, a
// monitor and the browser all read a success.
func TestAPageIsAnsweredWithTheStatusItSays(t *testing.T) {
	for _, status := range []int{
		stdhttp.StatusOK, stdhttp.StatusCreated, stdhttp.StatusNotFound, stdhttp.StatusConflict,
		stdhttp.StatusUnprocessableEntity, stdhttp.StatusTooManyRequests, stdhttp.StatusServiceUnavailable,
	} {
		view := &renderer{}
		rec := httptest.NewRecorder()
		ctx := hhttp.NewContext(rec, request(stdhttp.MethodGet, "/invoices/1"), view, nil)
		if err := ctx.ViewStatus(status, "invoices/missing", invoices{}); err != nil {
			t.Fatalf("ViewStatus(%d): %v", status, err)
		}
		if rec.Code != status || view.status != status || view.name != "invoices/missing" {
			t.Errorf("ViewStatus(%d) drew %q at %d and answered %d", status, view.name, view.status, rec.Code)
		}
		if rec.Header().Get("HX-Refresh") != "" {
			t.Errorf("ViewStatus(%d) told a browser that is not htmx to refresh", status)
		}
	}
}

// TestAStatusAPageCannotCarryIsRefusedBeforeAnythingIsWritten: 204, 205 and
// 304 have no body and 1xx is not an answer, so a page with one of them would
// be written and then dropped by the server.
func TestAStatusAPageCannotCarryIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	for _, status := range []int{0, 100, 199, 204, 205, 304, 600, 999} {
		view := &renderer{}
		rec := httptest.NewRecorder()
		ctx := hhttp.NewContext(rec, request(stdhttp.MethodGet, "/"), view, nil)
		if err := ctx.ViewStatus(status, "home", nil); err == nil {
			t.Errorf("ViewStatus(%d) answered a page", status)
		}
		if view.name != "" || rec.Body.Len() != 0 {
			t.Errorf("ViewStatus(%d) wrote %q before refusing", status, rec.Body.String())
		}
	}
}

// TestAnErrorPageAskedForByHtmxReloadsTheScreen: htmx throws a 4xx or 5xx
// away, so the click did nothing at all. HX-Refresh is how Refuse already
// makes a refusal visible, and a page with that status gets the same answer.
func TestAnErrorPageAskedForByHtmxReloadsTheScreen(t *testing.T) {
	cases := map[int]string{stdhttp.StatusNotFound: "true", stdhttp.StatusServiceUnavailable: "true", stdhttp.StatusCreated: ""}
	for status, want := range cases {
		rec := httptest.NewRecorder()
		r := request(stdhttp.MethodGet, "/invoices/1")
		r.Header.Set("HX-Request", "true")
		ctx := hhttp.NewContext(rec, r, &renderer{}, nil)
		if err := ctx.ViewStatus(status, "invoices/missing", nil); err != nil {
			t.Fatalf("ViewStatus(%d): %v", status, err)
		}
		if got := rec.Header().Get("HX-Refresh"); got != want {
			t.Errorf("ViewStatus(%d) to htmx set HX-Refresh %q, want %q", status, got, want)
		}
		if rec.Code != status {
			t.Errorf("ViewStatus(%d) to htmx answered %d: the status is the same for every client", status, rec.Code)
		}
	}
}

// TestAPageWithAStatusCarriesItIntoTheValues: a client that draws for itself
// sees the same status a browser does.
func TestAPageWithAStatusCarriesItIntoTheValues(t *testing.T) {
	rec := httptest.NewRecorder()
	r := request(stdhttp.MethodGet, "/invoices/1")
	r.Header.Set("Accept", hhttp.ViewDataMediaType)
	if err := hhttp.NewContext(rec, r, &renderer{}, nil).ViewStatus(stdhttp.StatusNotFound, "invoices/missing", invoices{}); err != nil {
		t.Fatalf("ViewStatus: %v", err)
	}
	if rec.Code != stdhttp.StatusNotFound {
		t.Errorf("answered %d, want 404", rec.Code)
	}
}
