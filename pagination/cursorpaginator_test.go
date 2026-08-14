package pagination_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/pagination"
)

type post struct{ ID int }

// postKey is what a repository writes: the value of every column the ORDER BY
// names, keyed by column name.
func postKey(p post) map[string]string {
	return map[string]string{"id": strconv.Itoa(p.ID)}
}

// descending returns count posts counting down from first, which is the order a
// backward keyset query returns rows in.
func descending(first, count int) []post {
	out := make([]post, count)
	for i := range out {
		out[i] = post{ID: first - i}
	}
	return out
}

// ascending returns count posts counting up from first.
func ascending(first, count int) []post {
	out := make([]post, count)
	for i := range out {
		out[i] = post{ID: first + i}
	}
	return out
}

func ids(items []post) []int {
	out := make([]int, len(items))
	for i, p := range items {
		out[i] = p.ID
	}
	return out
}

func TestCursorPaginateFirstPage(t *testing.T) {
	p := pagination.CursorPaginate(ascending(1, 11), 10, nil, postKey, pagination.Options{Path: "/posts"})

	if got := ids(p.Items()); !slices.Equal(got, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
		t.Errorf("Items = %v, want 1..10 with the probe row dropped", got)
	}
	if !p.OnFirstPage() {
		t.Error("OnFirstPage = false, want true")
	}
	if p.PreviousCursor() != nil {
		t.Error("PreviousCursor on the first page is not nil")
	}
	next := p.NextCursor()
	if next == nil {
		t.Fatal("NextCursor = nil, want the last row")
	}
	if parameterOf(next, "id") != "10" || !next.PointsToNextItems() {
		t.Errorf("NextCursor = %+v, want id 10 pointing forward", next)
	}
	if !p.HasMorePages() || p.OnLastPage() {
		t.Error("a page with a probe row has more pages")
	}
	if got, want := p.NextPageURL(), "/posts?cursor="+next.Encode(); got != want {
		t.Errorf("NextPageURL = %q, want %q", got, want)
	}
	if got := p.PreviousPageURL(); got != "" {
		t.Errorf("PreviousPageURL = %q, want empty", got)
	}
}

func TestCursorPaginateWholeResultSetFitsOnOnePage(t *testing.T) {
	p := pagination.CursorPaginate(ascending(1, 4), 10, nil, postKey, pagination.Options{Path: "/posts"})

	if p.NextCursor() != nil || p.PreviousCursor() != nil {
		t.Error("a result set that fits on one page has no cursors")
	}
	if p.HasPages() {
		t.Error("HasPages = true, want false")
	}
	if !p.OnLastPage() {
		t.Error("OnLastPage = false, want true")
	}
}

func TestCursorPaginateForwardPage(t *testing.T) {
	cursor := cursorPtr(map[string]string{"id": "10"}, true)
	p := pagination.CursorPaginate(ascending(11, 11), 10, cursor, postKey, pagination.Options{Path: "/posts"})

	if got := ids(p.Items()); !slices.Equal(got, []int{11, 12, 13, 14, 15, 16, 17, 18, 19, 20}) {
		t.Errorf("Items = %v, want 11..20", got)
	}
	if p.OnFirstPage() {
		t.Error("OnFirstPage = true, want false")
	}
	previous := p.PreviousCursor()
	if previous == nil {
		t.Fatal("PreviousCursor = nil, want the first row")
	}
	if parameterOf(previous, "id") != "11" || previous.PointsToNextItems() {
		t.Errorf("PreviousCursor = %+v, want id 11 pointing backward", previous)
	}
	next := p.NextCursor()
	if next == nil || parameterOf(next, "id") != "20" || !next.PointsToNextItems() {
		t.Errorf("NextCursor = %+v, want id 20 pointing forward", next)
	}
}

func TestCursorPaginateForwardLastPage(t *testing.T) {
	cursor := cursorPtr(map[string]string{"id": "20"}, true)
	p := pagination.CursorPaginate(ascending(21, 6), 10, cursor, postKey, pagination.Options{Path: "/posts"})

	if p.NextCursor() != nil {
		t.Error("NextCursor on the last page is not nil")
	}
	if p.PreviousCursor() == nil {
		t.Error("PreviousCursor = nil, want the first row: there is a page behind")
	}
	if !p.OnLastPage() {
		t.Error("OnLastPage = false, want true")
	}
}

// A backward query returns its rows the wrong way round, and the probe row is
// the one furthest from the boundary: the reader must still see the page in
// reading order.
func TestCursorPaginateBackwardPage(t *testing.T) {
	cursor := cursorPtr(map[string]string{"id": "31"}, false)
	p := pagination.CursorPaginate(descending(30, 11), 10, cursor, postKey, pagination.Options{Path: "/posts"})

	if got := ids(p.Items()); !slices.Equal(got, []int{21, 22, 23, 24, 25, 26, 27, 28, 29, 30}) {
		t.Errorf("Items = %v, want 21..30 in reading order", got)
	}
	previous := p.PreviousCursor()
	if previous == nil || parameterOf(previous, "id") != "21" || previous.PointsToNextItems() {
		t.Errorf("PreviousCursor = %+v, want id 21 pointing backward", previous)
	}
	next := p.NextCursor()
	if next == nil || parameterOf(next, "id") != "30" || !next.PointsToNextItems() {
		t.Errorf("NextCursor = %+v, want id 30 pointing forward", next)
	}
	if p.OnFirstPage() {
		t.Error("OnFirstPage = true, want false")
	}
}

// Walking back far enough to run out of rows lands on the first page, and the
// way forward has to stay open even though no probe row came back.
func TestCursorPaginateBackwardToTheStart(t *testing.T) {
	cursor := cursorPtr(map[string]string{"id": "6"}, false)
	p := pagination.CursorPaginate(descending(5, 5), 10, cursor, postKey, pagination.Options{Path: "/posts"})

	if got := ids(p.Items()); !slices.Equal(got, []int{1, 2, 3, 4, 5}) {
		t.Errorf("Items = %v, want 1..5 in reading order", got)
	}
	if !p.OnFirstPage() {
		t.Error("OnFirstPage = false, want true")
	}
	if p.PreviousCursor() != nil {
		t.Error("PreviousCursor at the start of the result set is not nil")
	}
	next := p.NextCursor()
	if next == nil || parameterOf(next, "id") != "5" {
		t.Errorf("NextCursor = %+v, want id 5: the page walked back from is still there", next)
	}
}

func TestCursorPaginateEmptyPage(t *testing.T) {
	cursor := cursorPtr(map[string]string{"id": "99"}, true)
	p := pagination.CursorPaginate(nil, 10, cursor, postKey, pagination.Options{Path: "/posts"})

	if p.Count() != 0 {
		t.Errorf("Count = %d, want 0", p.Count())
	}
	if p.NextCursor() != nil || p.PreviousCursor() != nil {
		t.Error("an empty page has no row to build a cursor from")
	}
	if p.NextPageURL() != "" || p.PreviousPageURL() != "" {
		t.Error("an empty page links nowhere")
	}
}

func TestCursorPaginateURLs(t *testing.T) {
	opts := pagination.Options{Path: "/posts", Query: map[string][]string{"team": {"core"}}}
	p := pagination.CursorPaginate(ascending(1, 11), 10, nil, postKey, opts)

	if got, want := p.URL(nil), "/posts?team=core"; got != want {
		t.Errorf("URL(nil) = %q, want %q", got, want)
	}
	encoded := p.NextCursor().Encode()
	if got, want := p.NextPageURL(), "/posts?cursor="+encoded+"&team=core"; got != want {
		t.Errorf("NextPageURL = %q, want %q", got, want)
	}
}

func TestCursorPaginateGuardsAgainstNonsensePageSize(t *testing.T) {
	p := pagination.CursorPaginate(ascending(1, 3), 0, nil, postKey, pagination.Options{})
	if got := p.PerPage(); got != 1 {
		t.Errorf("PerPage = %d, want 1", got)
	}
	if got := p.Count(); got != 1 {
		t.Errorf("Count = %d, want 1", got)
	}
}

func TestCursorPaginateWithoutKeyPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("CursorPaginate with a nil key did not panic")
		}
	}()
	pagination.CursorPaginate(ascending(1, 2), 10, nil, nil, pagination.Options{})
}

func TestThroughCursor(t *testing.T) {
	p := pagination.CursorPaginate(ascending(1, 11), 10, nil, postKey, pagination.Options{Path: "/posts"})
	mapped := pagination.ThroughCursor(p, func(v post) string { return strconv.Itoa(v.ID) })

	if got := mapped.Items(); !slices.Equal(got, []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}) {
		t.Errorf("Items = %v, want 1..10 as strings", got)
	}
	if mapped.NextPageURL() != p.NextPageURL() {
		t.Errorf("NextPageURL = %q, want %q", mapped.NextPageURL(), p.NextPageURL())
	}
	if mapped.Cursor() != p.Cursor() {
		t.Error("Through changed the cursor the page was read from")
	}
}

// getCursorForItem is how a repository asks for the cursor of a row it has in
// hand, rather than of the row at the edge of the page.
func TestGetCursorForItem(t *testing.T) {
	p := pagination.CursorPaginate(ascending(1, 11), 10, nil, postKey, pagination.Options{Path: "/posts"})

	parameters, err := p.GetParametersForItem(post{ID: 7})
	if err != nil {
		t.Fatalf("GetParametersForItem: %v", err)
	}
	if got, want := parameters["id"], "7"; got != want {
		t.Errorf("parameters[id] = %q, want %q", got, want)
	}

	cursor, err := p.GetCursorForItem(post{ID: 7}, true)
	if err != nil {
		t.Fatalf("GetCursorForItem: %v", err)
	}
	if !cursor.PointsToNextItems() {
		t.Error("PointsToNextItems = false, want true")
	}
	if got, _ := cursor.Parameter("id"); got != "7" {
		t.Errorf("Parameter(id) = %q, want \"7\"", got)
	}

	backward, err := p.GetCursorForItem(post{ID: 7}, false)
	if err != nil {
		t.Fatalf("GetCursorForItem backwards: %v", err)
	}
	if !backward.PointsToPreviousItems() {
		t.Error("PointsToPreviousItems = false, want true")
	}
}

// A mapped page keeps the cursors it computed and loses the key function, which
// was written against the row type that has just been mapped away.
func TestGetCursorForItemAfterThroughCursor(t *testing.T) {
	p := pagination.ThroughCursor(
		pagination.CursorPaginate(ascending(1, 11), 10, nil, postKey, pagination.Options{Path: "/posts"}),
		func(p post) int { return p.ID },
	)

	if _, err := p.GetCursorForItem(7, true); err == nil {
		t.Error("GetCursorForItem on a mapped page returned no error")
	}
	if p.NextCursor() == nil {
		t.Error("NextCursor = nil, want the cursor computed before the mapping")
	}
}

func TestCursorNameMovesTheQueryParameter(t *testing.T) {
	p := pagination.CursorPaginate(ascending(1, 11), 10, nil, postKey, pagination.Options{Path: "/posts"})

	if got, want := p.GetCursorName(), pagination.DefaultCursorName; got != want {
		t.Errorf("GetCursorName = %q, want %q", got, want)
	}

	p.SetCursorName("comments")
	if got, want := p.GetCursorName(), "comments"; got != want {
		t.Errorf("GetCursorName = %q, want %q", got, want)
	}
	if got := p.NextPageURL(); !strings.Contains(got, "comments=") {
		t.Errorf("NextPageURL = %q, want the cursor under the name that was set", got)
	}
}

func TestCursorPaginatorCarriesTheQueryStringOntoItsURLs(t *testing.T) {
	p := pagination.CursorPaginate(ascending(1, 11), 10, nil, postKey,
		pagination.Options{Path: "/posts"})

	// appends() drops the cursor parameter, because the paginator writes that
	// one itself -- the `if ($key !== $this->cursorName)` in addQuery().
	p.Appends(map[string]string{"sort": "newest", "cursor": "hijacked"}).
		Appends("tag", "go").
		Fragment("results")

	next := p.NextPageURL()
	for _, want := range []string{"sort=newest", "tag=go", "cursor=", "#results"} {
		if !strings.Contains(next, want) {
			t.Fatalf("next page URL %q is missing %q", next, want)
		}
	}
	if strings.Contains(next, "hijacked") {
		t.Fatalf("next page URL %q carries the appended cursor, and the paginator owns that parameter", next)
	}
}

func TestCursorPaginatorWithQueryStringDropsTheCursor(t *testing.T) {
	p := pagination.CursorPaginate(ascending(1, 11), 10, nil, postKey,
		pagination.Options{Path: "/posts"})

	p.WithQueryString(map[string][]string{
		"sort":   {"newest"},
		"cursor": {"hijacked"},
	})

	next := p.NextPageURL()
	if !strings.Contains(next, "sort=newest") {
		t.Fatalf("next page URL %q lost the request's query string", next)
	}
	if strings.Contains(next, "hijacked") {
		t.Fatalf("next page URL %q carries the request's cursor, which is the one being replaced", next)
	}
}

func TestCursorPaginatorPathIsFluent(t *testing.T) {
	p := pagination.CursorPaginate(ascending(1, 11), 10, nil, postKey, pagination.Options{})

	if got := p.WithPath("/archive").Path(); got != "/archive" {
		t.Fatalf("Path = %q after WithPath", got)
	}
	if got := p.SetPath("/posts").Path(); got != "/posts" {
		t.Fatalf("Path = %q after SetPath", got)
	}
	if !strings.HasPrefix(p.NextPageURL(), "/posts") {
		t.Fatalf("next page URL %q does not use the path that was set", p.NextPageURL())
	}
	if got := p.GetOptions().Path; got != "/posts" {
		t.Fatalf("GetOptions().Path = %q", got)
	}
}

func TestCursorPaginatorCollectionAccessors(t *testing.T) {
	p := pagination.CursorPaginate(ascending(1, 11), 10, nil, postKey, pagination.Options{Path: "/posts"})

	if p.IsEmpty() || !p.IsNotEmpty() {
		t.Fatal("a page of ten rows reports itself empty")
	}
	if got := len(p.GetCollection()); got != 10 {
		t.Fatalf("GetCollection has %d rows, want 10", got)
	}

	seen := 0
	for i, item := range p.GetIterator() {
		if item.ID != i+1 {
			t.Fatalf("row %d is %d", i, item.ID)
		}
		seen++
	}
	if seen != 10 {
		t.Fatalf("the iterator yielded %d rows, want 10", seen)
	}

	// setCollection replaces the rows and leaves the cursors alone: they were
	// computed from the rows the database returned.
	next := p.NextCursor().Encode()
	p.SetCollection(ascending(100, 2))
	if got := ids(p.Items()); !slices.Equal(got, []int{100, 101}) {
		t.Fatalf("Items = %v after SetCollection", got)
	}
	if p.NextCursor().Encode() != next {
		t.Fatal("SetCollection moved the next cursor")
	}

	empty := pagination.CursorPaginate([]post(nil), 10, nil, postKey, pagination.Options{})
	if !empty.IsEmpty() || empty.IsNotEmpty() {
		t.Fatal("a page of no rows does not report itself empty")
	}
}

func TestCursorPaginatorToArrayCarriesBothCursors(t *testing.T) {
	first := pagination.CursorPaginate(ascending(1, 11), 10, nil, postKey,
		pagination.Options{Path: "/posts"})

	got := first.ToArray()
	// The keys are Illuminate's, and prev_cursor is null on the first page --
	// PHP's `$this->previousCursor()?->encode()`.
	if got["prev_cursor"] != nil {
		t.Fatalf("prev_cursor = %v on the first page", got["prev_cursor"])
	}
	if got["prev_page_url"] != nil {
		t.Fatalf("prev_page_url = %v on the first page", got["prev_page_url"])
	}
	if got["next_cursor"] != first.NextCursor().Encode() {
		t.Fatalf("next_cursor = %v", got["next_cursor"])
	}
	if got["per_page"] != 10 {
		t.Fatalf("per_page = %v", got["per_page"])
	}
	if got["path"] != "/posts" {
		t.Fatalf("path = %v", got["path"])
	}

	body, err := first.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	for _, want := range []string{`"next_cursor"`, `"prev_cursor":null`, `"data"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("the JSON payload %s is missing %q", body, want)
		}
	}

	pretty, err := first.ToPrettyJSON()
	if err != nil {
		t.Fatalf("ToPrettyJSON: %v", err)
	}
	if !strings.Contains(string(pretty), "\n    ") {
		t.Fatalf("ToPrettyJSON is not indented four spaces: %s", pretty)
	}

	// MarshalJSON is jsonSerialize: the paginator itself is encodable.
	direct, err := first.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(direct) != string(body) {
		t.Fatalf("MarshalJSON and ToJSON disagree:\n%s\n%s", direct, body)
	}
}
