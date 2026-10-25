package conformance

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/pagination"
)

// conformanceTicket is the entity the listing test reads through the model
// layer.
type conformanceTicket struct {
	model.Model

	ID       string `db:"id"`
	TenantID string `db:"tenant_id"`
	Title    string `db:"title"`
	Status   string `db:"status"`
	Kind     string `db:"kind"`
}

// testAListingNarrowsOrdersAndCountsInsideTheTenant is the SQL a table's
// listing writes, run on a real engine through both builders: the
// case-insensitive "contains" with its escaped wildcards, the "in" of a facet,
// the order and its tie-break, the total, the page, the count of each facet
// value without the facet's own selection, and the tenant on every one of those
// statements. What the engines could disagree on is the LIKE and its ESCAPE, the
// GROUP BY over an alias, and the case folding, and the rows are chosen so that
// each of those shows up as a wrong set of ids.
func testAListingNarrowsOrdersAndCountsInsideTheTenant(t *testing.T, db *database.DB) {
	ctx := context.Background()
	name := table("listing")
	drop(t, db, name)
	t.Cleanup(func() { drop(t, db, name) })

	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE %s (id %s PRIMARY KEY, tenant_id %s NOT NULL, title %s NOT NULL, status %s NOT NULL, kind %s NOT NULL)`,
		name, database.KeyText, database.KeyText, database.KeyText, database.KeyText, database.KeyText)); err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, row := range [][5]string{
		{"1", "acme", "Alpha Report", "open", "a"},
		{"2", "acme", "beta report", "open", "b"},
		{"3", "acme", "Gamma 100% done", "done", "a"},
		{"4", "acme", "delta_x", "done", "b"},
		{"5", "acme", "plain", "open", "c"},
		{"6", "acme", "ALPHA two", "open", "a"},
		{"7", "globex", "alpha of the other tenant", "open", "a"},
		{"8", "globex", "100% theirs", "done", "a"},
	} {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id, tenant_id, title, status, kind) VALUES (?, ?, ?, ?, ?)`, name),
			row[0], row[1], row[2], row[3], row[4]); err != nil {
			t.Fatalf("insert %s: %v", row[0], err)
		}
	}

	listing, err := pagination.NewListing(pagination.ListingSpec{
		Search:    []string{"title"},
		Sorts:     map[string]string{"kind": "kind", "title": "title"},
		TieBreak:  "id",
		PageSizes: []int{2, 10},
		Facets: []pagination.FacetSpec{
			{Key: "status", Column: "status", Values: []string{"open", "done"}},
			{Key: "kind", Column: "kind", Values: []string{"a", "b", "c"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := func(values url.Values) pagination.ListingState { return listing.Parse(values) }

	tickets := model.NewTable(model.TableSpec{
		Name:         name,
		New:          func() model.Entity { return new(conformanceTicket) },
		ManualKey:    true,
		NoTimestamps: true,
	})
	acme, globex := auth.SystemGrant("ticket.list", "acme"), auth.SystemGrant("ticket.list", "globex")

	type outcome struct {
		ids    []string
		total  int
		counts map[string][]pagination.FacetCount
		page   int
	}
	// through runs the same listing through the query builder and through the
	// model builder: both must answer alike.
	through := func(t *testing.T, g auth.Grant, s pagination.ListingState) map[string]outcome {
		t.Helper()
		out := map[string]outcome{}

		records, result, err := query.NewBuilder(db, db.GetQueryGrammar(), db.GetPostProcessor()).From(name).
			Listing(ctx, g, s, "/tickets")
		if err != nil {
			t.Fatalf("query listing: %v", err)
		}
		o := outcome{total: result.Total(), page: result.State.Page(), counts: map[string][]pagination.FacetCount{
			"status": result.FacetCounts("status"), "kind": result.FacetCounts("kind"),
		}}
		for _, record := range records {
			o.ids = append(o.ids, fmt.Sprint(record["id"]))
		}
		out["query"] = o

		rows, result, err := tickets.Query(db).Listing(ctx, g, s, "/tickets")
		if err != nil {
			t.Fatalf("model listing: %v", err)
		}
		o = outcome{total: result.Total(), page: result.State.Page(), counts: map[string][]pagination.FacetCount{
			"status": result.FacetCounts("status"), "kind": result.FacetCounts("kind"),
		}}
		for _, row := range rows {
			o.ids = append(o.ids, row.(*conformanceTicket).ID)
		}
		out["model"] = o
		return out
	}

	cases := []struct {
		name  string
		grant auth.Grant
		query url.Values
		ids   []string
		total int
	}{
		{"every row of the tenant, by id", acme, nil, []string{"1", "2", "3", "4", "5", "6"}, 6},
		{"a search ignores case and stays in the tenant", acme, url.Values{"q": {"alpha"}}, []string{"1", "6"}, 2},
		{"a percent in the search is a percent", acme, url.Values{"q": {"100%"}}, []string{"3"}, 1},
		{"a lone percent is not a wildcard", acme, url.Values{"q": {"%"}}, []string{"3"}, 1},
		{"a lone underscore is not a wildcard", acme, url.Values{"q": {"_"}}, []string{"4"}, 1},
		{"an exclamation mark is plain text", acme, url.Values{"q": {"!"}}, nil, 0},
		{"the other tenant sees only its own", globex, url.Values{"q": {"alpha"}}, []string{"7"}, 1},
		{"a facet", acme, url.Values{"status": {"done"}}, []string{"3", "4"}, 2},
		{"two facets are anded", acme, url.Values{"status": {"open"}, "kind": {"a"}}, []string{"1", "6"}, 2},
		{"the values of one facet are ored", acme, url.Values{"kind": {"a", "b"}}, []string{"1", "2", "3", "4", "6"}, 5},
		{"a search and a facet", acme, url.Values{"q": {"report"}, "status": {"open"}}, []string{"1", "2"}, 2},
		{"a sort breaks its ties by the key", acme, url.Values{"sort": {"kind"}}, []string{"1", "3", "6", "2", "4", "5"}, 6},
		{"a descending sort reverses the tie-break too", acme, url.Values{"sort": {"kind"}, "dir": {"desc"}}, []string{"5", "4", "2", "6", "3", "1"}, 6},
		{"a page of two", acme, url.Values{"per_page": {"2"}, "page": {"2"}}, []string{"3", "4"}, 6},
		{"a page past the end is the last page", acme, url.Values{"per_page": {"2"}, "page": {"99"}}, []string{"5", "6"}, 6},
		{"a sort that was not declared is the default order", acme,
			url.Values{"sort": {"id; drop table " + name}, "dir": {"x"}}, []string{"1", "2", "3", "4", "5", "6"}, 6},
	}
	for _, c := range cases {
		for builder, o := range through(t, c.grant, state(c.query)) {
			if !slices.Equal(o.ids, c.ids) || o.total != c.total {
				t.Errorf("%s (%s): ids %v total %d, want ids %v total %d", c.name, builder, o.ids, o.total, c.ids, c.total)
			}
		}
	}

	// The facet counts leave out the facet's own selection and keep every other
	// filter, and the other tenant's rows are in none of them.
	counted := []struct {
		name   string
		query  url.Values
		status []pagination.FacetCount
		kind   []pagination.FacetCount
	}{
		{"no filter", nil,
			[]pagination.FacetCount{{Value: "open", Count: 4}, {Value: "done", Count: 2}},
			[]pagination.FacetCount{{Value: "a", Count: 3}, {Value: "b", Count: 2}, {Value: "c", Count: 1}}},
		{"a search", url.Values{"q": {"alpha"}},
			[]pagination.FacetCount{{Value: "open", Count: 2}, {Value: "done", Count: 0}},
			[]pagination.FacetCount{{Value: "a", Count: 2}, {Value: "b", Count: 0}, {Value: "c", Count: 0}}},
		{"a status counts the statuses as if it were not picked", url.Values{"status": {"done"}},
			[]pagination.FacetCount{{Value: "open", Count: 4}, {Value: "done", Count: 2, Selected: true}},
			[]pagination.FacetCount{{Value: "a", Count: 1}, {Value: "b", Count: 1}, {Value: "c", Count: 0}}},
		{"a status and a kind", url.Values{"status": {"open"}, "kind": {"a"}},
			[]pagination.FacetCount{{Value: "open", Count: 2, Selected: true}, {Value: "done", Count: 1}},
			[]pagination.FacetCount{{Value: "a", Count: 2, Selected: true}, {Value: "b", Count: 1}, {Value: "c", Count: 1}}},
	}
	for _, c := range counted {
		for builder, o := range through(t, acme, state(c.query)) {
			if !reflect.DeepEqual(o.counts["status"], c.status) {
				t.Errorf("%s (%s): status counts %+v, want %+v", c.name, builder, o.counts["status"], c.status)
			}
			if !reflect.DeepEqual(o.counts["kind"], c.kind) {
				t.Errorf("%s (%s): kind counts %+v, want %+v", c.name, builder, o.counts["kind"], c.kind)
			}
		}
	}

	// A sort naming a column nobody declared left the table where it was.
	var left int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+name).Scan(&left); err != nil || left != 8 {
		t.Fatalf("the table after the hostile sort: %d rows, %v", left, err)
	}
}
