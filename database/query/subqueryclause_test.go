package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// A subquery in a from, a join or a select list is the same leak the union and
// the `where exists` subquery already were: the inner select is compiled whole,
// so a filter on the outer query says which of its rows survive and nothing
// about which rows went in. An aggregate, a distinct or a limit inside the
// subquery has looked at every customer by then.
//
// Each of these builds the statement and reads it back for one tenant filter per
// query.

func subFor(b *query.Builder, connection *fakeConnection, table string) *query.Builder {
	return query.NewBuilder(connection, b.GetGrammar(), b.GetProcessor()).From(table)
}

func TestASubqueryInFromCarriesTheTenantToo(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)

	b.FromSub(subFor(b, connection, "invoices").Where("total", ">", 1000), "i")

	if _, err := b.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := connection.calls[0].sql
	if got := countTenantFilters(sql); got != 2 {
		t.Errorf("the statement compares the tenant %d time(s), want 2 -- one for the outer query and one for the subquery it reads from:\n%s", got, sql)
	}
	if got, want := strings.Count(sql, "?"), len(connection.calls[0].bindings); got != want {
		t.Fatalf("the statement has %d placeholders and %d bindings:\n%s\n%v", got, want, sql, connection.calls[0].bindings)
	}
	// from comes before where, and the subquery's own tenant comes before its
	// value: acme, 1000, acme.
	if got, want := connection.calls[0].bindings, []any{"acme", 1000, "acme"}; !sameBindings(got, want) {
		t.Errorf("the bindings are %v, want %v", got, want)
	}
}

func TestASubqueryInAJoinCarriesTheTenantToo(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)

	b.JoinSub(subFor(b, connection, "invoices").Where("total", ">", 1000), "i",
		"i.user_id", "=", "users.id", "left", false)

	if _, err := b.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := connection.calls[0].sql
	if got := countTenantFilters(sql); got != 2 {
		t.Errorf("the statement compares the tenant %d time(s), want 2 -- the joined subquery needs one of its own:\n%s", got, sql)
	}
	if !strings.Contains(sql, "left join") {
		t.Errorf("the join type was lost:\n%s", sql)
	}
}

func TestASubqueryInACrossJoinCarriesTheTenantToo(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)

	b.CrossJoinSub(subFor(b, connection, "plans"), "p")

	if _, err := b.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := connection.calls[0].sql
	if got := countTenantFilters(sql); got != 2 {
		t.Errorf("the statement compares the tenant %d time(s), want 2 -- a cross join pairs every row of the subquery with every row of this one:\n%s", got, sql)
	}
}

func TestASubqueryInALateralJoinCarriesTheTenantToo(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)

	b.LeftJoinLateral(subFor(b, connection, "invoices").Limit(1), "latest")

	if _, err := b.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := connection.calls[0].sql
	if got := countTenantFilters(sql); got != 2 {
		t.Errorf("the statement compares the tenant %d time(s), want 2:\n%s", got, sql)
	}
	if !strings.Contains(sql, "join lateral") {
		t.Errorf("the join was not compiled as a lateral one:\n%s", sql)
	}
}

// A select subquery is the one whose bindings are easiest to get wrong: they
// live in the select segment, ahead of everything else, so a tenant added to it
// pushes every later value along.
func TestASubqueryInTheSelectListCarriesTheTenantAndKeepsTheBindingsLinedUp(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)

	b.Select("id").
		SelectRaw("? as label", "invoices").
		SelectSub(subFor(b, connection, "invoices").Where("total", ">", 1000), "spend").
		SelectRaw("? as tail", "after").
		Where("state", "=", "active")

	if _, err := b.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}

	call := connection.calls[0]
	if got := countTenantFilters(call.sql); got != 2 {
		t.Errorf("the statement compares the tenant %d time(s), want 2:\n%s", got, call.sql)
	}
	if got, want := strings.Count(call.sql, "?"), len(call.bindings); got != want {
		t.Fatalf("the statement has %d placeholders and %d bindings:\n%s\n%v", got, want, call.sql, call.bindings)
	}
	want := []any{"invoices", "acme", 1000, "after", "acme", "active"}
	if !sameBindings(call.bindings, want) {
		t.Errorf("the bindings are %v, want %v -- the values around the subquery slid when it gained one", call.bindings, want)
	}
}

// The tenant belongs to the statement and not to the builder. Running the same
// query under two Grants has to produce two statements, and neither of them may
// carry the other's filter or a second copy of its own.
func TestRunningASubqueryClauseTwiceScopesItOnceEachTime(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)
	b.FromSub(subFor(b, connection, "invoices"), "i")

	ctx := context.Background()
	if _, err := b.Get(ctx, grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, err := b.Get(ctx, auth.SystemGrant("invoice.read", "globex")); err != nil {
		t.Fatalf("Get: %v", err)
	}

	first, second := connection.calls[0], connection.calls[1]
	if got := countTenantFilters(second.sql); got != 2 {
		t.Errorf("the second statement compares the tenant %d time(s), want 2:\n%s", got, second.sql)
	}
	if len(first.bindings) != len(second.bindings) {
		t.Fatalf("the two statements bind %d and %d values:\n%v\n%v",
			len(first.bindings), len(second.bindings), first.bindings, second.bindings)
	}
	for _, binding := range second.bindings {
		if binding == "acme" {
			t.Errorf("the second statement carries the first Grant's tenant: %v", second.bindings)
		}
	}
}

func TestASubqueryClauseIsNotAWayPastTheTenantCheck(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)
	b.FromSub(subFor(b, connection, "invoices"), "i")

	if _, err := b.Get(context.Background(), auth.Grant{}); err == nil {
		t.Fatal("a query reading from a subquery ran without a tenant")
	}
	if len(connection.calls) != 0 {
		t.Errorf("a statement reached the connection anyway: %v", connection.calls)
	}
}

// A string subquery cannot be scoped, and the builder does not pretend
// otherwise: it compiles what it was handed. The outer query still carries its
// own tenant.
func TestASubqueryGivenAsSQLIsCompiledAsWritten(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection)
	b.FromSub("select 1", "one")

	if _, err := b.Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := countTenantFilters(connection.calls[0].sql); got != 1 {
		t.Errorf("the statement compares the tenant %d time(s), want 1:\n%s", got, connection.calls[0].sql)
	}
}

func sameBindings(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
