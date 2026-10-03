package pagination_test

import (
	"slices"
	"strconv"
	"strings"
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
	p := pagination.NewLengthAwarePage(10, 512, 10, 3, pagination.Options{Path: "/users"})

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
		p := pagination.NewLengthAwarePage(0, c.total, c.perPage, 1, pagination.Options{})
		if got := p.LastPage(); got != c.want {
			t.Errorf("LastPage(total=%d, perPage=%d) = %d, want %d", c.total, c.perPage, got, c.want)
		}
	}
}

func TestPaginateEmptyPageHasNoItemRange(t *testing.T) {
	p := pagination.NewLengthAwarePage(0, 0, 10, 1, pagination.Options{})
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
	p := pagination.NewLengthAwarePage(3, 30, 0, -4, pagination.Options{})
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
	p := pagination.NewLengthAwarePage(0, 20, 10, 7, pagination.Options{Path: "/users"})
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

	first := pagination.NewLengthAwarePage(10, 100, 10, 1, opts)
	if got := first.PreviousPageURL(); got != "" {
		t.Errorf("PreviousPageURL on page one = %q, want empty", got)
	}
	if got, want := first.NextPageURL(), "/users?page=2"; got != want {
		t.Errorf("NextPageURL = %q, want %q", got, want)
	}

	last := pagination.NewLengthAwarePage(10, 100, 10, 10, opts)
	if got, want := last.PreviousPageURL(), "/users?page=9"; got != want {
		t.Errorf("PreviousPageURL = %q, want %q", got, want)
	}
	if got := last.NextPageURL(); got != "" {
		t.Errorf("NextPageURL on the last page = %q, want empty", got)
	}
}

func TestPaginateURLClampsBelowOne(t *testing.T) {
	p := pagination.NewLengthAwarePage(10, 100, 10, 1, pagination.Options{Path: "/users"})
	if got, want := p.URL(-5), "/users?page=1"; got != want {
		t.Errorf("URL(-5) = %q, want %q", got, want)
	}
}

func TestLinksFitWithoutSeparator(t *testing.T) {
	// Thirteen pages is one below the width at which the window collapses.
	p := pagination.NewLengthAwarePage(10, 130, 10, 5, pagination.Options{Path: "/users"})
	want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
	if got := pages(p.Links()); !slices.Equal(got, want) {
		t.Errorf("Links = %v, want %v", got, want)
	}
}

func TestLinksNearTheBeginning(t *testing.T) {
	p := pagination.NewLengthAwarePage(10, 140, 10, 1, pagination.Options{Path: "/users"})
	want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 0, 13, 14}
	if got := pages(p.Links()); !slices.Equal(got, want) {
		t.Errorf("Links = %v, want %v", got, want)
	}
}

func TestLinksNearTheEnd(t *testing.T) {
	p := pagination.NewLengthAwarePage(10, 140, 10, 14, pagination.Options{Path: "/users"})
	want := []int{1, 2, 0, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}
	if got := pages(p.Links()); !slices.Equal(got, want) {
		t.Errorf("Links = %v, want %v", got, want)
	}
}

func TestLinksInTheMiddle(t *testing.T) {
	p := pagination.NewLengthAwarePage(10, 300, 10, 15, pagination.Options{Path: "/users"})
	want := []int{1, 2, 0, 12, 13, 14, 15, 16, 17, 18, 0, 29, 30}
	if got := pages(p.Links()); !slices.Equal(got, want) {
		t.Errorf("Links = %v, want %v", got, want)
	}
}

func TestLinksOnEachSide(t *testing.T) {
	opts := pagination.Options{Path: "/users", OnEachSide: 1}
	p := pagination.NewLengthAwarePage(10, 300, 10, 15, opts)
	want := []int{1, 2, 0, 14, 15, 16, 0, 29, 30}
	if got := pages(p.Links()); !slices.Equal(got, want) {
		t.Errorf("Links = %v, want %v", got, want)
	}
}

// Zero means the default, so asking for no neighbours at all takes a negative.
func TestLinksNegativeOnEachSideMeansNone(t *testing.T) {
	opts := pagination.Options{Path: "/users", OnEachSide: -1}
	p := pagination.NewLengthAwarePage(10, 300, 10, 15, opts)
	want := []int{1, 2, 0, 15, 0, 29, 30}
	if got := pages(p.Links()); !slices.Equal(got, want) {
		t.Errorf("Links = %v, want %v", got, want)
	}
}

func TestLinksShape(t *testing.T) {
	p := pagination.NewLengthAwarePage(10, 140, 10, 1, pagination.Options{Path: "/users"})
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
	p := pagination.NewLengthAwarePage(3, 3, 10, 1, pagination.Options{Path: "/users"})
	if got := pages(p.Links()); !slices.Equal(got, []int{1}) {
		t.Errorf("Links = %v, want [1]", got)
	}
}

// The page holds no rows, so the rows reach the payload through the argument,
// and a page encoded on its own is the arithmetic and nothing else.
func TestLengthAwarePagePayloadCarriesTheRowsItIsGiven(t *testing.T) {
	p := pagination.NewLengthAwarePage(2, 12, 2, 3, pagination.Options{Path: "/users"})

	payload := p.ToArray([]string{"a", "b"})
	if got, ok := payload["data"].([]string); !ok || !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("data = %v, want the rows handed to ToArray", payload["data"])
	}
	if payload["total"] != 12 || payload["last_page"] != 6 || payload["current_page"] != 3 {
		t.Errorf("payload = %v, want total 12, last page 6, current page 3", payload)
	}
	if payload["from"] != 5 || payload["to"] != 6 {
		t.Errorf("from/to = %v/%v, want 5/6", payload["from"], payload["to"])
	}

	body, err := p.ToJSON([]string{"a", "b"})
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	if !strings.Contains(string(body), `"data":["a","b"]`) {
		t.Errorf("ToJSON = %s, want the rows under data", body)
	}

	alone, err := p.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if strings.Contains(string(alone), `"data"`) {
		t.Errorf("MarshalJSON = %s, want no data key: the page holds no rows", alone)
	}
	if !strings.Contains(string(alone), `"total":12`) {
		t.Errorf("MarshalJSON = %s, want the arithmetic", alone)
	}
}
