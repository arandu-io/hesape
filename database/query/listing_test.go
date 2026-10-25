package query_test

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/pagination"
)

func noteListing(t *testing.T) *pagination.Listing {
	t.Helper()
	listing, err := pagination.NewListing(pagination.ListingSpec{
		Search:      []string{"title", "notes.body"},
		Sorts:       map[string]string{"title": "title", "created": "created_at"},
		DefaultSort: "created",
		DefaultDir:  pagination.SortDesc,
		TieBreak:    "id",
		Facets: []pagination.FacetSpec{
			{Key: "status", Column: "status", Values: []string{"open", "done"}},
			{Key: "kind", Column: "kind", Values: []string{"a", "b", "c"}},
		},
		PageSizes: []int{10, 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	return listing
}

func noteState(t *testing.T, raw string) pagination.ListingState {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return noteListing(t).Parse(values)
}

func countRow(n int64) []query.Record { return []query.Record{{"aggregate": n}} }

func facetRows(pairs ...any) []query.Record {
	var rows []query.Record
	for i := 0; i < len(pairs); i += 2 {
		rows = append(rows, query.Record{"facet_value": pairs[i], "facet_count": pairs[i+1]})
	}
	return rows
}

// TestTheListingAppliesSearchFacetsAndOrderToTheStatement reads the statement:
// the search is a group of ORs over the declared columns, every value is bound,
// and the order ends in the tie-break.
func TestTheListingAppliesSearchFacetsAndOrderToTheStatement(t *testing.T) {
	connection := &fakeConnection{}
	state := noteState(t, "q=hello&status=open&kind=a&kind=c&sort=title&dir=desc")
	if _, err := newTestBuilder(connection).ApplyListing(state).Get(context.Background(), grant()); err != nil {
		t.Fatal(err)
	}

	want := `select * from "users" where "users"."tenant_id" = ? and ((lower("title") like lower(?) escape '!' or lower("notes"."body") like lower(?) escape '!') and "status" in (?) and "kind" in (?, ?)) order by "title" desc, "id" desc`
	if got := connection.lastSQL(); got != want {
		t.Fatalf("statement:\n got: %s\nwant: %s", got, want)
	}
	wantBindings := []any{"acme", "%hello%", "%hello%", "open", "a", "c"}
	if got := connection.lastBindings(); !reflect.DeepEqual(got, wantBindings) {
		t.Fatalf("bindings = %#v, want %#v", got, wantBindings)
	}
}

func TestTheSearchEscapesItsWildcards(t *testing.T) {
	connection := &fakeConnection{}
	state := noteState(t, url.Values{"q": {`50%_off!`}}.Encode())
	if _, err := newTestBuilder(connection).ApplyListing(state).Get(context.Background(), grant()); err != nil {
		t.Fatal(err)
	}
	if got := connection.lastBindings()[1]; got != `%50!%!_off!!%` {
		t.Fatalf("pattern = %q, want the wildcards escaped with !", got)
	}
}

// TestASortOutsideTheSpecNeverReachesTheStatement: the request names a sort and
// a column, and the statement orders by neither unless the spec declared it.
func TestASortOutsideTheSpecNeverReachesTheStatement(t *testing.T) {
	for _, hostile := range []string{"password", "title; drop table users", "users.password", `"title"`, "title) desc, (select"} {
		connection := &fakeConnection{}
		state := noteState(t, url.Values{"sort": {hostile}, "dir": {hostile}, "status": {hostile}}.Encode())
		if _, err := newTestBuilder(connection).ApplyListing(state).Get(context.Background(), grant()); err != nil {
			t.Fatal(err)
		}
		sql := connection.lastSQL()
		if want := ` order by "created_at" desc, "id" desc`; !strings.HasSuffix(sql, want) {
			t.Errorf("sort=%q: statement %q does not end with %q", hostile, sql, want)
		}
		if strings.Contains(sql, "password") || strings.Contains(sql, "drop") || strings.Contains(sql, "select * from \"users\" where \"users\".\"tenant_id\" = ? and (") {
			t.Errorf("sort=%q reached the statement: %s", hostile, sql)
		}
		for _, binding := range connection.lastBindings() {
			if binding == hostile {
				t.Errorf("the hostile value %q was bound", hostile)
			}
		}
	}
}

func TestApplyingAListingDoesNotChangeTheQueryItWasBuiltFrom(t *testing.T) {
	base := newTestBuilder(&fakeConnection{}).Where("archived", false)
	_ = base.Clone().ApplyListing(noteState(t, "q=x&status=open"))
	if len(base.Wheres) != 1 || len(base.Orders) != 0 {
		t.Fatalf("the base query gained %d wheres and %d orders", len(base.Wheres), len(base.Orders))
	}
}

// TestEveryStatementOfAListingCarriesTheTenant: the page, the total and the
// count of each facet are separate statements, and a tenant missing from one of
// them is another customer's rows counted into this customer's filter.
func TestEveryStatementOfAListingCarriesTheTenant(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		countRow(3),
		{{"id": int64(1)}, {"id": int64(2)}, {"id": int64(3)}},
		facetRows("open", int64(2), "done", int64(1)),
		facetRows("a", int64(3)),
	}}
	state := noteState(t, "q=x&status=open")
	rows, result, err := newTestBuilder(connection).Listing(context.Background(), auth.SystemGrant("note.list", "globex"), state, "/notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || result.Total() != 3 {
		t.Fatalf("rows = %d, total = %d", len(rows), result.Total())
	}
	if len(connection.calls) != 4 {
		t.Fatalf("a listing with two facets is four statements, got %d", len(connection.calls))
	}
	for i, call := range connection.calls {
		if !strings.Contains(call.sql, `"users"."tenant_id" = ?`) {
			t.Errorf("statement %d carries no tenant: %s", i, call.sql)
		}
		if len(call.bindings) == 0 || call.bindings[0] != "globex" {
			t.Errorf("statement %d binds %#v, want the tenant of the Grant first", i, call.bindings)
		}
	}
}

// TestAFacetCountLeavesItsOwnSelectionOut is the faceted behaviour of the
// reference: picking "open" does not hide how many "done" rows there are.
func TestAFacetCountLeavesItsOwnSelectionOut(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		countRow(2),
		{{"id": int64(1)}, {"id": int64(2)}},
		facetRows("open", int64(2), "done", int64(5)),
		facetRows("a", int64(1), "b", int64(1)),
	}}
	state := noteState(t, "q=x&status=open&kind=a")
	_, result, err := newTestBuilder(connection).Listing(context.Background(), grant(), state, "/notes")
	if err != nil {
		t.Fatal(err)
	}

	statusSQL, kindSQL := connection.calls[2].sql, connection.calls[3].sql
	if strings.Contains(statusSQL, `"status" in`) {
		t.Errorf("the status count filters by status itself:\n%s", statusSQL)
	}
	if !strings.Contains(statusSQL, `"kind" in (?)`) || !strings.Contains(statusSQL, "like lower(?)") {
		t.Errorf("the status count dropped the other filters:\n%s", statusSQL)
	}
	if strings.Contains(kindSQL, `"kind" in`) {
		t.Errorf("the kind count filters by kind itself:\n%s", kindSQL)
	}
	if !strings.Contains(kindSQL, `"status" in (?)`) {
		t.Errorf("the kind count dropped the status filter:\n%s", kindSQL)
	}
	if !strings.Contains(statusSQL, `select "status" as "facet_value", count(*) as facet_count`) &&
		!strings.Contains(statusSQL, `facet_count`) {
		t.Errorf("not a count query:\n%s", statusSQL)
	}
	if !strings.Contains(statusSQL, `group by "status"`) {
		t.Errorf("the status count is not grouped:\n%s", statusSQL)
	}
	if strings.Contains(statusSQL, "order by") || strings.Contains(statusSQL, "limit") {
		t.Errorf("a count is ordered or limited:\n%s", statusSQL)
	}

	wantStatus := []pagination.FacetCount{{Value: "open", Count: 2, Selected: true}, {Value: "done", Count: 5}}
	if got := result.FacetCounts("status"); !reflect.DeepEqual(got, wantStatus) {
		t.Errorf("status counts = %+v, want %+v", got, wantStatus)
	}
	wantKind := []pagination.FacetCount{{Value: "a", Count: 1, Selected: true}, {Value: "b", Count: 1}, {Value: "c"}}
	if got := result.FacetCounts("kind"); !reflect.DeepEqual(got, wantKind) {
		t.Errorf("kind counts = %+v, want %+v (a value no row carries counts zero)", got, wantKind)
	}
}

func TestFacetCountsReadWhatTheDriversReturn(t *testing.T) {
	facet := pagination.FacetSpec{Key: "s", Column: "s", Values: []string{"open", "done", "x"}}
	rows := []query.Record{
		{"facet_value": []byte("open"), "facet_count": "4"},
		{"facet_value": "done", "facet_count": int64(2)},
		{"facet_value": "unlisted", "facet_count": int64(9)},
		{"facet_value": nil, "facet_count": int64(7)},
	}
	got := query.FacetCountsFrom(facet, []string{"done"}, rows)
	want := []pagination.FacetCount{{Value: "open", Count: 4}, {Value: "done", Count: 2, Selected: true}, {Value: "x"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}
}

func TestAPageThePastTheEndIsBroughtBackToTheLastPage(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		countRow(45),
		{{"id": int64(41)}},
		facetRows(), facetRows(),
	}}
	state := noteState(t, "page=9&per_page=20")
	_, result, err := newTestBuilder(connection).Listing(context.Background(), grant(), state, "/notes")
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Page() != 3 || result.Page.CurrentPage() != 3 || result.Page.LastPage() != 3 {
		t.Fatalf("page = %d/%d of %d, want 3 of 3", result.State.Page(), result.Page.CurrentPage(), result.Page.LastPage())
	}
	if sql := connection.calls[1].sql; !strings.HasSuffix(sql, "limit 20 offset 40") {
		t.Fatalf("the page query is %s", sql)
	}
	if got := result.Page.URL(2); got != "/notes?page=2&per_page=20" {
		t.Fatalf("URL(2) = %q", got)
	}
	if result.Page.FirstItem() != 41 || result.Page.LastItem() != 41 {
		t.Fatalf("items %d to %d", result.Page.FirstItem(), result.Page.LastItem())
	}
}

func TestAListingWithNoRowsReadsNoPage(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{countRow(0), facetRows(), facetRows()}}
	rows, result, err := newTestBuilder(connection).Listing(context.Background(), grant(), noteState(t, "q=nothing&page=4"), "/notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 || result.Total() != 0 || result.State.Page() != 1 {
		t.Fatalf("rows=%d total=%d page=%d", len(rows), result.Total(), result.State.Page())
	}
	if len(connection.calls) != 3 {
		t.Fatalf("no rows means no page query: %d statements", len(connection.calls))
	}
	if got := result.FacetCounts("status"); len(got) != 2 || got[0].Count != 0 {
		t.Fatalf("an empty listing still lists every value: %+v", got)
	}
}

func TestAListingNeedsAGrantWithATenantAndAStateOfAListing(t *testing.T) {
	connection := &fakeConnection{}
	if _, _, err := newTestBuilder(connection).Listing(context.Background(), auth.Grant{}, noteState(t, ""), "/notes"); err == nil {
		t.Fatal("a Grant that carries no tenant must be refused")
	}
	if len(connection.calls) != 0 {
		t.Fatalf("the connection received %d calls without a tenant", len(connection.calls))
	}
	if _, _, err := newTestBuilder(connection).Listing(context.Background(), grant(), pagination.ListingState{}, "/notes"); !errors.Is(err, query.ErrNoListing) {
		t.Fatalf("the zero state: error = %v, want ErrNoListing", err)
	}
	if len(connection.calls) != 0 {
		t.Fatalf("the connection received %d calls for a zero state", len(connection.calls))
	}
}

func TestAnErrorOnAFacetCountIsReturned(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{countRow(0)}}
	connection.err = nil
	failing := &failAfter{fakeConnection: connection, after: 1}
	b := query.NewBuilder(failing, &fakeGrammar{}, &fakeProcessor{}).From("users")
	if _, _, err := b.Listing(context.Background(), grant(), noteState(t, ""), "/notes"); err == nil {
		t.Fatal("a failing facet count must fail the listing")
	}
}

// failAfter lets the first selects through and then fails.
type failAfter struct {
	*fakeConnection
	after int
}

func (f *failAfter) Select(ctx context.Context, sql string, bindings []any, useReadPDO bool) ([]query.Record, error) {
	if len(f.calls) >= f.after {
		f.calls = append(f.calls, call{kind: "select", sql: sql, bindings: bindings})
		return nil, errors.New("connection lost")
	}
	return f.fakeConnection.Select(ctx, sql, bindings, useReadPDO)
}

func TestTheFacetCountQueryOfAnUnknownFacetIsRefused(t *testing.T) {
	b := newTestBuilder(&fakeConnection{})
	if q, _, ok := b.FacetCountQuery(noteState(t, ""), "password"); ok || q != nil {
		t.Fatal("a facet the spec did not declare must not build a query")
	}
	if _, _, ok := b.FacetCountQuery(pagination.ListingState{}, "status"); ok {
		t.Fatal("the zero state declares no facet")
	}
}
