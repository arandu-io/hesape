package pagination_test

import (
	"encoding/json"
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

// readCursorPage is what a repository does with the rows a keyset query read:
// the page is built from how many came back, and the rows are trimmed and put in
// reading order the way the page says.
func readCursorPage(read []post, perPage int, cursor *pagination.Cursor, opts pagination.Options) ([]post, *pagination.CursorPage) {
	p := pagination.NewCursorPage(len(read), perPage, cursor, func(i int) map[string]string { return postKey(read[i]) }, opts)
	items := slices.Clone(read[:p.Count()])
	if p.Reversed() {
		slices.Reverse(items)
	}
	return items, p
}

func TestCursorPaginateFirstPage(t *testing.T) {
	items, p := readCursorPage(ascending(1, 11), 10, nil, signedOptions("/posts"))

	if got := ids(items); !slices.Equal(got, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
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
	if got := p.NextPageURL(); parameterOf(cursorIn(t, got), "id") != "10" {
		t.Errorf("NextPageURL = %q, want a link to the cursor at id 10", got)
	}
	if got := p.PreviousPageURL(); got != "" {
		t.Errorf("PreviousPageURL = %q, want empty", got)
	}
}

func TestCursorPaginateWholeResultSetFitsOnOnePage(t *testing.T) {
	_, p := readCursorPage(ascending(1, 4), 10, nil, signedOptions("/posts"))

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
	items, p := readCursorPage(ascending(11, 11), 10, cursor, signedOptions("/posts"))

	if got := ids(items); !slices.Equal(got, []int{11, 12, 13, 14, 15, 16, 17, 18, 19, 20}) {
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
	_, p := readCursorPage(ascending(21, 6), 10, cursor, signedOptions("/posts"))

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
	items, p := readCursorPage(descending(30, 11), 10, cursor, signedOptions("/posts"))

	if got := ids(items); !slices.Equal(got, []int{21, 22, 23, 24, 25, 26, 27, 28, 29, 30}) {
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
	items, p := readCursorPage(descending(5, 5), 10, cursor, signedOptions("/posts"))

	if got := ids(items); !slices.Equal(got, []int{1, 2, 3, 4, 5}) {
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
	_, p := readCursorPage(nil, 10, cursor, signedOptions("/posts"))

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
	opts := signedOptions("/posts")
	opts.Query = map[string][]string{"team": {"core"}}
	_, p := readCursorPage(ascending(1, 11), 10, nil, opts)

	if got, want := p.URL(nil), "/posts?team=core"; got != want {
		t.Errorf("URL(nil) = %q, want %q", got, want)
	}
	next := p.NextPageURL()
	if !strings.HasPrefix(next, "/posts?cursor=") || !strings.HasSuffix(next, "&team=core") {
		t.Errorf("NextPageURL = %q, want the cursor and the query carried on", next)
	}
	if got := parameterOf(cursorIn(t, next), "id"); got != "10" {
		t.Errorf("the cursor in %q is at id %q, want 10", next, got)
	}
}

func TestCursorPaginateWithoutSignerPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewCursorPage without a signer did not panic")
		}
	}()
	pagination.NewCursorPage(2, 10, nil, func(int) map[string]string { return nil }, pagination.Options{Path: "/posts"})
}

func TestCursorPaginateGuardsAgainstNonsensePageSize(t *testing.T) {
	_, p := readCursorPage(ascending(1, 3), 0, nil, signedOptions(""))
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
			t.Error("NewCursorPage with a nil key did not panic")
		}
	}()
	pagination.NewCursorPage(2, 10, nil, nil, signedOptions(""))
}

func TestCursorNameMovesTheQueryParameter(t *testing.T) {
	_, p := readCursorPage(ascending(1, 11), 10, nil, signedOptions("/posts"))

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
	_, p := readCursorPage(ascending(1, 11), 10, nil, signedOptions("/posts"))

	// Appends drops the cursor parameter, because the paginator writes that
	// one itself.
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
	_, p := readCursorPage(ascending(1, 11), 10, nil, signedOptions("/posts"))

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
	_, p := readCursorPage(ascending(1, 11), 10, nil, signedOptions(""))

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

func TestCursorPageReportsWhetherItIsEmpty(t *testing.T) {
	_, p := readCursorPage(ascending(1, 11), 10, nil, signedOptions("/posts"))

	if p.IsEmpty() || !p.IsNotEmpty() {
		t.Fatal("a page of ten rows reports itself empty")
	}
	if got := p.Count(); got != 10 {
		t.Fatalf("Count = %d, want 10", got)
	}

	_, empty := readCursorPage([]post(nil), 10, nil, signedOptions(""))
	if !empty.IsEmpty() || empty.IsNotEmpty() {
		t.Fatal("a page of no rows does not report itself empty")
	}
}

func TestCursorPaginatorToArrayCarriesBothCursors(t *testing.T) {
	items, first := readCursorPage(ascending(1, 11), 10, nil, signedOptions("/posts"))

	got := first.ToArray(items)
	// prev_cursor is null on the first page, because there is no previous
	// cursor to encode.
	if got["prev_cursor"] != nil {
		t.Fatalf("prev_cursor = %v on the first page", got["prev_cursor"])
	}
	if got["prev_page_url"] != nil {
		t.Fatalf("prev_page_url = %v on the first page", got["prev_page_url"])
	}
	token, ok := got["next_cursor"].(string)
	if !ok {
		t.Fatalf("next_cursor = %v, want the token the next page is read with", got["next_cursor"])
	}
	read, err := cursors.FromEncoded(token)
	if err != nil {
		t.Fatalf("next_cursor does not verify: %v", err)
	}
	if id := parameterOf(&read, "id"); id != "10" {
		t.Fatalf("next_cursor is at id %q, want 10", id)
	}
	if got["per_page"] != 10 {
		t.Fatalf("per_page = %v", got["per_page"])
	}
	if got["path"] != "/posts" {
		t.Fatalf("path = %v", got["path"])
	}

	body, err := first.ToJSON(items)
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	for _, want := range []string{`"next_cursor"`, `"prev_cursor":null`, `"data"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("the JSON payload %s is missing %q", body, want)
		}
	}

	pretty, err := first.ToPrettyJSON(items)
	if err != nil {
		t.Fatalf("ToPrettyJSON: %v", err)
	}
	if !strings.Contains(string(pretty), "\n    ") {
		t.Fatalf("ToPrettyJSON is not indented four spaces: %s", pretty)
	}

	// MarshalJSON encodes the page itself, without the rows it does not hold. Cursor
	// tokens contain an expiry timestamp, so two serializations are compared by
	// meaning rather than bytes: crossing a wall-clock second legitimately
	// changes the signature while preserving the boundary.
	direct, err := first.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	for name, encoded := range map[string][]byte{"ToJSON": body, "MarshalJSON": direct} {
		var payload struct {
			NextCursor string  `json:"next_cursor"`
			PrevCursor *string `json:"prev_cursor"`
			PerPage    int     `json:"per_page"`
			Path       string  `json:"path"`
		}
		if err := json.Unmarshal(encoded, &payload); err != nil {
			t.Fatalf("%s payload: %v", name, err)
		}
		cursor, err := cursors.FromEncoded(payload.NextCursor)
		if err != nil || parameterOf(&cursor, "id") != "10" || payload.PrevCursor != nil || payload.PerPage != 10 || payload.Path != "/posts" {
			t.Fatalf("%s changed the paginator semantics: %#v err=%v", name, payload, err)
		}
	}
}
