package pagination_test

import (
	"net/url"
	"slices"
	"testing"

	"github.com/arandu-io/hesape/pagination"
)

// window is the paginator every test in this file takes a window of: a result
// set long enough that the pages do not all fit, read at the given page.
func window(page, total int) *pagination.LengthAwarePaginator[string] {
	return pagination.Paginate(rows(10), total, 10, page, pagination.Options{Path: "/orders"})
}

// keys reads the page numbers of one range back, sorted, since a Go map has no
// order and the page numbers are what the arithmetic is worth asserting on.
func keys(urls map[int]string) []int {
	out := make([]int, 0, len(urls))
	for page := range urls {
		out = append(out, page)
	}
	slices.Sort(out)
	return out
}

// Below onEachSide*2+8 pages every page fits, so the window is the whole range
// and there is nothing to leave out.
func TestMakeReturnsTheSmallSlider(t *testing.T) {
	got := pagination.Make(window(3, 120))

	if want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}; !slices.Equal(keys(got.First), want) {
		t.Errorf("First = %v, want %v", keys(got.First), want)
	}
	if got.Slider != nil || got.Last != nil {
		t.Errorf("Slider = %v and Last = %v, want neither", got.Slider, got.Last)
	}
	if url, want := got.First[2], "/orders?page=2"; url != want {
		t.Errorf("the URL of page 2 = %q, want %q", url, want)
	}
}

// Close to the beginning there is no room for a slider on the left, so the
// opening run is widened and only the last two pages cap the end.
func TestMakeReturnsTheSliderTooCloseToTheBeginning(t *testing.T) {
	got := pagination.Make(window(2, 400))

	if want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}; !slices.Equal(keys(got.First), want) {
		t.Errorf("First = %v, want %v", keys(got.First), want)
	}
	if got.Slider != nil {
		t.Errorf("Slider = %v, want none", got.Slider)
	}
	if want := []int{39, 40}; !slices.Equal(keys(got.Last), want) {
		t.Errorf("Last = %v, want %v", keys(got.Last), want)
	}
}

// Close to the end it is the other way round.
func TestMakeReturnsTheSliderTooCloseToTheEnding(t *testing.T) {
	got := pagination.Make(window(39, 400))

	if want := []int{1, 2}; !slices.Equal(keys(got.First), want) {
		t.Errorf("First = %v, want %v", keys(got.First), want)
	}
	if got.Slider != nil {
		t.Errorf("Slider = %v, want none", got.Slider)
	}
	if want := []int{31, 32, 33, 34, 35, 36, 37, 38, 39, 40}; !slices.Equal(keys(got.Last), want) {
		t.Errorf("Last = %v, want %v", keys(got.Last), want)
	}
}

// With room on both sides all three pieces are there, which is the sliding
// window a numbered pager is recognised by.
func TestMakeReturnsTheFullSlider(t *testing.T) {
	got := pagination.Make(window(20, 400))

	if want := []int{1, 2}; !slices.Equal(keys(got.First), want) {
		t.Errorf("First = %v, want %v", keys(got.First), want)
	}
	if want := []int{17, 18, 19, 20, 21, 22, 23}; !slices.Equal(keys(got.Slider), want) {
		t.Errorf("Slider = %v, want %v", keys(got.Slider), want)
	}
	if want := []int{39, 40}; !slices.Equal(keys(got.Last), want) {
		t.Errorf("Last = %v, want %v", keys(got.Last), want)
	}
}

func TestURLWindowPieces(t *testing.T) {
	w := pagination.NewURLWindow(window(20, 400))

	if want := []int{1, 2}; !slices.Equal(keys(w.GetStart()), want) {
		t.Errorf("GetStart = %v, want %v", keys(w.GetStart()), want)
	}
	if want := []int{39, 40}; !slices.Equal(keys(w.GetFinish()), want) {
		t.Errorf("GetFinish = %v, want %v", keys(w.GetFinish()), want)
	}
	if want := []int{19, 20, 21}; !slices.Equal(keys(w.GetAdjacentURLRange(1)), want) {
		t.Errorf("GetAdjacentURLRange(1) = %v, want %v", keys(w.GetAdjacentURLRange(1)), want)
	}
	if !w.HasPages() {
		t.Error("HasPages = false, want true")
	}
	if pagination.NewURLWindow(window(1, 4)).HasPages() {
		t.Error("HasPages on a single page = true, want false")
	}
}

func TestGetURLRange(t *testing.T) {
	p := window(3, 120)

	got := p.GetURLRange(2, 4)
	if want := []int{2, 3, 4}; !slices.Equal(keys(got), want) {
		t.Errorf("GetURLRange = %v, want %v", keys(got), want)
	}
	if url, want := got[4], "/orders?page=4"; url != want {
		t.Errorf("the URL of page 4 = %q, want %q", url, want)
	}
	if got := p.GetURLRange(5, 2); got != nil {
		t.Errorf("GetURLRange of an empty range = %v, want nil", got)
	}
}

func TestResolveCurrentPath(t *testing.T) {
	u, err := url.Parse("https://example.test/orders?page=3&sort=due#table")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	if got, want := pagination.ResolveCurrentPath(u, "/"), "https://example.test/orders"; got != want {
		t.Errorf("ResolveCurrentPath = %q, want %q", got, want)
	}
	if got, want := pagination.ResolveCurrentPath(nil, "/"), "/"; got != want {
		t.Errorf("ResolveCurrentPath(nil) = %q, want %q", got, want)
	}
}

func TestResolveQueryString(t *testing.T) {
	u, err := url.Parse("/orders?sort=due&page=3")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	if got, want := pagination.ResolveQueryString(u, nil).Get("sort"), "due"; got != want {
		t.Errorf("ResolveQueryString sort = %q, want %q", got, want)
	}
	if got := pagination.ResolveQueryString(nil, url.Values{"sort": {"name"}}); got.Get("sort") != "name" {
		t.Errorf("ResolveQueryString(nil) = %v, want the default", got)
	}
}

// The view names are global: set once where the application is wired, read by
// whatever renders.
func TestTheDefaultViewIsTailwindAndTheSwitchesMoveIt(t *testing.T) {
	t.Cleanup(pagination.UseTailwind)

	if got, want := pagination.DefaultView(), pagination.TailwindView; got != want {
		t.Errorf("DefaultView = %q, want %q", got, want)
	}
	if got, want := pagination.DefaultSimpleView(), pagination.SimpleTailwindView; got != want {
		t.Errorf("DefaultSimpleView = %q, want %q", got, want)
	}

	pagination.UseBootstrapFive()
	if got, want := pagination.DefaultView(), pagination.BootstrapFiveView; got != want {
		t.Errorf("DefaultView after UseBootstrapFive = %q, want %q", got, want)
	}
	if got, want := pagination.DefaultSimpleView(), pagination.SimpleBootstrapFiveView; got != want {
		t.Errorf("DefaultSimpleView after UseBootstrapFive = %q, want %q", got, want)
	}

	// useBootstrap() is useBootstrapFour() under its older name.
	pagination.UseBootstrap()
	if got, want := pagination.DefaultView(), pagination.BootstrapFourView; got != want {
		t.Errorf("DefaultView after UseBootstrap = %q, want %q", got, want)
	}

	pagination.UseBootstrapThree()
	if got, want := pagination.DefaultView(), pagination.BootstrapThreeView; got != want {
		t.Errorf("DefaultView after UseBootstrapThree = %q, want %q", got, want)
	}

	if got, want := pagination.DefaultView(pagination.SemanticUIView), pagination.SemanticUIView; got != want {
		t.Errorf("DefaultView(semantic) = %q, want %q", got, want)
	}
	if got, want := pagination.DefaultSimpleView("pagination::plain"), "pagination::plain"; got != want {
		t.Errorf("DefaultSimpleView(plain) = %q, want %q", got, want)
	}
}
