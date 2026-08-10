package pagination_test

import (
	"slices"
	"strconv"
	"testing"

	"github.com/arandu-io/hesape/pagination"
)

// rows returns n placeholder items, which is all a paginator needs to count.
func rows(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "row-" + strconv.Itoa(i)
	}
	return out
}

// pages reads a rendered window back as page numbers, with zero for a
// separator, which is the shape the arithmetic is worth asserting on.
func pages(links []pagination.Link) []int {
	out := make([]int, len(links))
	for i, l := range links {
		out[i] = l.Page
	}
	return out
}

func TestPaginateCounts(t *testing.T) {
	p := pagination.Paginate(rows(10), 512, 10, 3, pagination.Options{Path: "/users"})

	if got := p.Total(); got != 512 {
		t.Errorf("Total = %d, want 512", got)
	}
	if got := p.PerPage(); got != 10 {
		t.Errorf("PerPage = %d, want 10", got)
	}
	if got := p.CurrentPage(); got != 3 {
		t.Errorf("CurrentPage = %d, want 3", got)
	}
	if got := p.LastPage(); got != 52 {
		t.Errorf("LastPage = %d, want 52", got)
	}
	if got := p.Count(); got != 10 {
		t.Errorf("Count = %d, want 10", got)
	}
	if got, want := p.FirstItem(), 21; got != want {
		t.Errorf("FirstItem = %d, want %d", got, want)
	}
	if got, want := p.LastItem(), 30; got != want {
		t.Errorf("LastItem = %d, want %d", got, want)
	}
}

func TestPaginateLastPageRoundsUp(t *testing.T) {
	cases := []struct {
		total, perPage, want int
	}{
		{0, 10, 1},
		{1, 10, 1},
		{10, 10, 1},
		{11, 10, 2},
		{99, 10, 10},
		{100, 10, 10},
		{101, 10, 11},
	}
	for _, c := range cases {
		p := pagination.Paginate(rows(0), c.total, c.perPage, 1, pagination.Options{})
		if got := p.LastPage(); got != c.want {
			t.Errorf("LastPage(total=%d, perPage=%d) = %d, want %d", c.total, c.perPage, got, c.want)
		}
	}
}

func TestPaginateEmptyPageHasNoItemRange(t *testing.T) {
	p := pagination.Paginate(rows(0), 0, 10, 1, pagination.Options{})
	if got := p.FirstItem(); got != 0 {
		t.Errorf("FirstItem = %d, want 0", got)
	}
	if got := p.LastItem(); got != 0 {
		t.Errorf("LastItem = %d, want 0", got)
	}
	if p.HasPages() {
		t.Error("HasPages = true, want false")
	}
	if !p.OnFirstPage() || !p.OnLastPage() {
		t.Error("an empty result set is on the first page and on the last")
	}
}

// A page size of zero comes from an unset configuration value, and dividing by
// it is a panic in production rather than a wrong number on a screen.
func TestPaginateGuardsAgainstNonsenseInput(t *testing.T) {
	p := pagination.Paginate(rows(3), 30, 0, -4, pagination.Options{})
	if got := p.PerPage(); got != 1 {
		t.Errorf("PerPage = %d, want 1", got)
	}
	if got := p.CurrentPage(); got != 1 {
		t.Errorf("CurrentPage = %d, want 1", got)
	}
	if got := p.LastPage(); got != 30 {
		t.Errorf("LastPage = %d, want 30", got)
	}
}

// The reader who deleted the last row of the last page lands here. The page is
// empty and every link on it still works.
func TestPaginatePastTheEnd(t *testing.T) {
	p := pagination.Paginate(rows(0), 20, 10, 7, pagination.Options{Path: "/users"})
	if got := p.CurrentPage(); got != 7 {
		t.Errorf("CurrentPage = %d, want 7", got)
	}
	if p.HasMorePages() {
		t.Error("HasMorePages = true, want false")
	}
	if got, want := p.PreviousPageURL(), "/users?page=6"; got != want {
		t.Errorf("PreviousPageURL = %q, want %q", got, want)
	}
}

func TestPaginateNeighbourURLs(t *testing.T) {
	opts := pagination.Options{Path: "/users"}

	first := pagination.Paginate(rows(10), 100, 10, 1, opts)
	if got := first.PreviousPageURL(); got != "" {
		t.Errorf("PreviousPageURL on page one = %q, want empty", got)
	}
	if got, want := first.NextPageURL(), "/users?page=2"; got != want {
		t.Errorf("NextPageURL = %q, want %q", got, want)
	}

	last := pagination.Paginate(rows(10), 100, 10, 10, opts)
	if got, want := last.PreviousPageURL(), "/users?page=9"; got != want {
		t.Errorf("PreviousPageURL = %q, want %q", got, want)
	}
	if got := last.NextPageURL(); got != "" {
		t.Errorf("NextPageURL on the last page = %q, want empty", got)
	}
}

func TestPaginateURLClampsBelowOne(t *testing.T) {
	p := pagination.Paginate(rows(10), 100, 10, 1, pagination.Options{Path: "/users"})
	if got, want := p.URL(-5), "/users?page=1"; got != want {
		t.Errorf("URL(-5) = %q, want %q", got, want)
	}
}

func TestLinksFitWithoutSeparator(t *testing.T) {
	// Thirteen pages is one below the width at which the window collapses.
	p := pagination.Paginate(rows(10), 130, 10, 5, pagination.Options{Path: "/users"})
	want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
	if got := pages(p.Links()); !slices.Equal(got, want) {
		t.Errorf("Links = %v, want %v", got, want)
	}
}

func TestLinksNearTheBeginning(t *testing.T) {
	p := pagination.Paginate(rows(10), 140, 10, 1, pagination.Options{Path: "/users"})
	want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 0, 13, 14}
	if got := pages(p.Links()); !slices.Equal(got, want) {
		t.Errorf("Links = %v, want %v", got, want)
	}
}

func TestLinksNearTheEnd(t *testing.T) {
	p := pagination.Paginate(rows(10), 140, 10, 14, pagination.Options{Path: "/users"})
	want := []int{1, 2, 0, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}
	if got := pages(p.Links()); !slices.Equal(got, want) {
		t.Errorf("Links = %v, want %v", got, want)
	}
}

func TestLinksInTheMiddle(t *testing.T) {
	p := pagination.Paginate(rows(10), 300, 10, 15, pagination.Options{Path: "/users"})
	want := []int{1, 2, 0, 12, 13, 14, 15, 16, 17, 18, 0, 29, 30}
	if got := pages(p.Links()); !slices.Equal(got, want) {
		t.Errorf("Links = %v, want %v", got, want)
	}
}

func TestLinksOnEachSide(t *testing.T) {
	opts := pagination.Options{Path: "/users", OnEachSide: 1}
	p := pagination.Paginate(rows(10), 300, 10, 15, opts)
	want := []int{1, 2, 0, 14, 15, 16, 0, 29, 30}
	if got := pages(p.Links()); !slices.Equal(got, want) {
		t.Errorf("Links = %v, want %v", got, want)
	}
}

// Zero means the default, so asking for no neighbours at all takes a negative.
func TestLinksNegativeOnEachSideMeansNone(t *testing.T) {
	opts := pagination.Options{Path: "/users", OnEachSide: -1}
	p := pagination.Paginate(rows(10), 300, 10, 15, opts)
	want := []int{1, 2, 0, 15, 0, 29, 30}
	if got := pages(p.Links()); !slices.Equal(got, want) {
		t.Errorf("Links = %v, want %v", got, want)
	}
}

func TestLinksShape(t *testing.T) {
	p := pagination.Paginate(rows(10), 140, 10, 1, pagination.Options{Path: "/users"})
	links := p.Links()

	if links[0].Label != "1" || links[0].URL != "/users?page=1" || !links[0].Active {
		t.Errorf("first link = %+v, want the active page one", links[0])
	}
	if links[1].Active {
		t.Errorf("second link = %+v, want inactive", links[1])
	}

	separator := links[10]
	if separator.Label != pagination.Separator {
		t.Errorf("separator label = %q, want %q", separator.Label, pagination.Separator)
	}
	if separator.URL != "" || separator.Page != 0 || separator.Active {
		t.Errorf("separator = %+v, want no URL, no page and inactive", separator)
	}
}

func TestLinksSinglePage(t *testing.T) {
	p := pagination.Paginate(rows(3), 3, 10, 1, pagination.Options{Path: "/users"})
	if got := pages(p.Links()); !slices.Equal(got, []int{1}) {
		t.Errorf("Links = %v, want [1]", got)
	}
}

func TestThroughMapsItemsAndKeepsTheArithmetic(t *testing.T) {
	p := pagination.Paginate([]int{1, 2, 3}, 33, 3, 4, pagination.Options{Path: "/users", Fragment: "list"})
	mapped := pagination.Through(p, strconv.Itoa)

	if got := mapped.Items(); !slices.Equal(got, []string{"1", "2", "3"}) {
		t.Errorf("Items = %v, want [1 2 3] as strings", got)
	}
	if mapped.Total() != p.Total() || mapped.LastPage() != p.LastPage() || mapped.CurrentPage() != p.CurrentPage() {
		t.Error("Through changed the arithmetic")
	}
	if got, want := mapped.NextPageURL(), "/users?page=5#list"; got != want {
		t.Errorf("NextPageURL = %q, want %q", got, want)
	}
	if got := p.Items(); !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("the original items changed: %v", got)
	}
}
