package view_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/httpx"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/view"
)

// homeData is the shape a view declares in its @go block, and the shape the
// controller has to pass. A different type is an error naming both sides.
type homeData struct{ Name string }

func init() {
	// What `aru view:build` emits for resources/views/home.kyse.go.
	view.Register("home", func(w io.Writer, data any) error {
		d, ok := data.(homeData)
		if !ok {
			return view.WrongData("home", "homeData", data)
		}
		_, err := io.WriteString(w, "<h1>Olá "+d.Name+"</h1>")
		return err
	})
}

// TestTheControllerRendersByName is the chain a controller sees: ctx.View with
// a name and a typed struct, and HTML comes out of the response.
//
// It drives the Context the router builds rather than the router itself. The
// router does not wire a renderer yet -- hesape/routing has no WithRenderer and
// hesape/httpx has no Action, both still landing -- and a test of this package
// must not wait on that. What the wiring being missed looks like is guarded
// where it actually happened: TestEveryAssetIsServed registers the module on a
// real router, because the assets shipped 404 for weeks precisely because every
// test called the handler and none called the route.
func TestTheControllerRendersByName(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := httpx.NewContext(rec, req, view.NewRenderer(), nil)

	if err := ctx.View("home", homeData{Name: "Paulo"}); err != nil {
		t.Fatal(err)
	}

	resp := rec.Result()
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := string(body); got != "<h1>Olá Paulo</h1>" {
		t.Errorf("body = %q", got)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
}

// TestAViewWithNoRendererSaysWhatToWire: without the view module registered,
// ctx.View has nothing to render with. The message has to name the line that is
// missing from bootstrap/app.go, because the alternative is a nil dereference
// pointing at the framework.
func TestAViewWithNoRendererSaysWhatToWire(t *testing.T) {
	// A Context built without a renderer, which is what an application that
	// never registered the view module hands every one of its handlers.
	ctx := &httpx.Context{Response: httptest.NewRecorder(), Request: httptest.NewRequest("GET", "/", nil)}
	err := ctx.View("home", homeData{})
	if err == nil {
		t.Fatal("rendering without a renderer succeeded")
	}
	// The file, and the call that goes in it. The wiring moved from
	// cmd/app/main.go to bootstrap/app.go with the Laravel tree, and a message
	// naming a file the project does not have is worse than no message.
	for _, want := range []string{"bootstrap/app.go", "view.NewModule()"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

// TestAnUnknownViewSuggests: a missing view is a typo, a file never built, or a
// name that drifted. Listing the near misses answers all three faster than the
// name alone.
func TestAnUnknownViewSuggests(t *testing.T) {
	err := view.NewRenderer().Render(context.Background(), httptest.NewRecorder(),
		http.StatusOK, "hom", nil)
	if err == nil {
		t.Fatal("an unknown view rendered")
	}
	if !strings.Contains(err.Error(), "home") {
		t.Errorf("the error does not suggest the near miss: %v", err)
	}
}

// TestWrongDataNamesBothSides: the controller and the view disagreeing about
// the data is the one failure a name-based render can still have. Rendering the
// zero value instead would be a blank page with a 200.
func TestWrongDataNamesBothSides(t *testing.T) {
	err := view.NewRenderer().Render(context.Background(), httptest.NewRecorder(),
		http.StatusOK, "home", "isto nao e homeData")
	if err == nil {
		t.Fatal("the wrong data type rendered")
	}
	for _, want := range []string{"homeData", "string"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

// TestTheRenderIsOnTheTimeline: "the page is slow because of the database" and
// "because of the markup" are two different afternoons, and the console is what
// tells them apart.
func TestTheRenderIsOnTheTimeline(t *testing.T) {
	col := log.NewCollector("req-1")
	ctx := log.WithCollector(context.Background(), col)

	if err := view.NewRenderer().Render(ctx, httptest.NewRecorder(),
		http.StatusOK, "home", homeData{Name: "x"}); err != nil {
		t.Fatal(err)
	}

	renders := col.Renders()
	if len(renders) != 1 || renders[0].Name != "home" {
		t.Fatalf("the render was not recorded: %+v", renders)
	}
}

// TestAFragmentCarriesItsStatus: a form that failed validation answers 422 with
// the form fragment, and HTMX swaps it in. Answering 200 would make the browser
// and the logs both believe it worked.
func TestAFragmentCarriesItsStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	ctx := httpx.NewContext(rec, req, view.NewRenderer(), nil)

	if err := ctx.Fragment(http.StatusUnprocessableEntity, "home", homeData{Name: "erro"}); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422", rec.Code)
	}
}

// TestAFailedRenderDoesNotAnswer200 is the bug an end-to-end run found: the
// renderer wrote the status before calling the template, so a view that failed
// halfway answered 200 with half a page. The error page cannot change a status
// already on the wire, and monitoring counts it as a success.
//
// It reached the browser as a type mismatch between a view and its layout,
// rendered as a 200 whose body was the error page.
func TestAFailedRenderDoesNotAnswer200(t *testing.T) {
	name := "half-written-page"
	view.Register(name, func(w io.Writer, data any) error {
		// Exactly the shape of a real failure: a layout writes its opening
		// markup, then the section inside it rejects the data.
		fmt.Fprint(w, "<html><body><h1>")
		return view.WrongData(name, "HomeData", data)
	})

	// httptest.NewRecorder starts at 200, so the question is not what the code
	// is -- it is whether anything reached the wire at all. Nothing may: the
	// error page owns the response from here.
	rec := &spyWriter{ResponseRecorder: httptest.NewRecorder()}
	err := view.NewRenderer().Render(context.Background(), rec, http.StatusOK, name, 42)

	if err == nil {
		t.Fatal("the render reported no error")
	}
	if rec.wroteHeader {
		t.Error("a failed render committed a status: the error page cannot correct one already written")
	}
	if rec.Body.Len() != 0 {
		t.Errorf("a failed render put %d bytes on the wire: %q", rec.Body.Len(), rec.Body.String())
	}
}

// spyWriter records whether the status was ever committed.
type spyWriter struct {
	*httptest.ResponseRecorder
	wroteHeader bool
}

func (s *spyWriter) WriteHeader(code int) {
	s.wroteHeader = true
	s.ResponseRecorder.WriteHeader(code)
}

func (s *spyWriter) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseRecorder.Write(b)
}
