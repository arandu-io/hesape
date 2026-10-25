package model

import (
	"context"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/pagination"
)

func notesListing(t *testing.T, raw string) pagination.ListingState {
	t.Helper()
	listing, err := pagination.NewListing(pagination.ListingSpec{
		Search:      []string{"body"},
		Sorts:       map[string]string{"body": "body"},
		DefaultSort: "body",
		TieBreak:    "id",
		Facets:      []pagination.FacetSpec{{Key: "kind", Column: "kind", Values: []string{"memo", "todo"}}},
		PageSizes:   []int{10},
	})
	if err != nil {
		t.Fatal(err)
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return listing.Parse(values)
}

// TestEveryStatementOfAModelListingCarriesTheTenantOfTheGrant: the page, the
// total and the count of each facet go through the same preparation as any read
// of the model, so each is filtered by the Grant's tenant -- the only place a
// tenant comes from.
func TestEveryStatementOfAModelListingCarriesTheTenantOfTheGrant(t *testing.T) {
	m, conn := sqliteNotes()
	conn.queue(query.Record{"aggregate": int64(1)})
	conn.queue(query.Record{"id": "n1", "tenant_id": "globex", "body": "hello", "kind": "memo"})
	conn.queue(query.Record{"facet_value": "memo", "facet_count": int64(4)}, query.Record{"facet_value": "todo", "facet_count": int64(2)})

	state := notesListing(t, "q=hello&kind=memo")
	rows, result, err := newQuery(m.base()).Listing(context.Background(), auth.SystemGrant("notes.list", "globex"), state, "/notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || result.Total() != 1 {
		t.Fatalf("rows = %d, total = %d", len(rows), result.Total())
	}
	want := []pagination.FacetCount{{Value: "memo", Count: 4, Selected: true}, {Value: "todo", Count: 2}}
	if got := result.FacetCounts("kind"); !reflect.DeepEqual(got, want) {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}

	if len(conn.statements) != 3 {
		t.Fatalf("a listing with one facet is three statements, got %d:\n%s", len(conn.statements), strings.Join(conn.sqls(), "\n"))
	}
	for i, s := range conn.statements {
		if !strings.Contains(s.SQL, `"notes"."tenant_id" = ?`) {
			t.Errorf("statement %d carries no tenant:\n%s", i, s.SQL)
		}
		found := false
		for _, binding := range s.Bindings {
			if binding == "globex" {
				found = true
			}
			if binding == "acme" {
				t.Errorf("statement %d binds another tenant", i)
			}
		}
		if !found {
			t.Errorf("statement %d does not bind the Grant's tenant: %#v", i, s.Bindings)
		}
	}
	if strings.Contains(conn.statements[2].SQL, `"kind" in`) {
		t.Errorf("the kind count filters by kind itself:\n%s", conn.statements[2].SQL)
	}
	if !strings.Contains(conn.statements[2].SQL, "group by") {
		t.Errorf("the kind count is not grouped:\n%s", conn.statements[2].SQL)
	}
	if !strings.Contains(conn.statements[1].SQL, `like lower(?) escape '!'`) {
		t.Errorf("the page does not search:\n%s", conn.statements[1].SQL)
	}
}

func TestAModelListingRefusesAMissingTenantAndAForeignState(t *testing.T) {
	m, conn := sqliteNotes()
	if _, _, err := newQuery(m.base()).Listing(context.Background(), auth.Grant{}, notesListing(t, ""), "/notes"); err == nil {
		t.Fatal("a Grant without a tenant must be refused")
	}
	if _, _, err := newQuery(m.base()).Listing(context.Background(), acmeNotes, pagination.ListingState{}, "/notes"); err != query.ErrNoListing {
		t.Fatalf("the zero state: error = %v, want ErrNoListing", err)
	}
	if len(conn.statements) != 0 {
		t.Fatalf("%d statements reached the connection", len(conn.statements))
	}
}

func TestApplyListingOnlyBuilds(t *testing.T) {
	m, conn := sqliteNotes()
	b := newQuery(m.base()).ApplyListing(notesListing(t, "q=x&kind=todo"))
	if len(conn.statements) != 0 {
		t.Fatal("ApplyListing ran a statement")
	}
	if len(b.GetQuery().Orders) != 2 || len(b.GetQuery().Wheres) != 2 {
		t.Fatalf("wheres = %d, orders = %d", len(b.GetQuery().Wheres), len(b.GetQuery().Orders))
	}
}
