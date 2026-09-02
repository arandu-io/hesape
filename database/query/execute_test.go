package query_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/pagination"
)

func grant() auth.Grant { return auth.SystemGrant("invoice.read", "acme") }

func TestReadTerminalsNeverCompileOrExecuteAnInvalidOperator(t *testing.T) {
	ctx := context.Background()
	actions := map[string]func(*query.Builder) error{
		"get": func(b *query.Builder) error {
			_, err := b.Get(ctx, grant())
			return err
		},
		"exists": func(b *query.Builder) error {
			_, err := b.Exists(ctx, grant())
			return err
		},
		"count": func(b *query.Builder) error {
			_, err := b.Count(ctx, grant())
			return err
		},
		"pagination count": func(b *query.Builder) error {
			_, err := b.GroupBy("plan").GetCountForPagination(ctx, grant())
			return err
		},
		"raw SQL": func(b *query.Builder) error {
			_, err := b.ToRawSQL(ctx, grant())
			return err
		},
		"cursor": func(b *query.Builder) error {
			for _, err := range b.Cursor(ctx, grant()) {
				return err
			}
			return nil
		},
	}

	for name, action := range actions {
		t.Run(name, func(t *testing.T) {
			connection := &fakeConnection{}
			grammar := &compileSpyGrammar{fakeGrammar: &fakeGrammar{}}
			b := query.NewBuilder(connection, grammar, &fakeProcessor{}).
				From("invoices").
				Where("id", "bogus", 1)

			err := action(b)
			if !errors.Is(err, query.ErrInvalidOperator) {
				t.Fatalf("error = %v, want ErrInvalidOperator", err)
			}
			if grammar.compileCalls != 0 {
				t.Fatalf("a compiler was called %d times", grammar.compileCalls)
			}
			if len(connection.calls) != 0 {
				t.Fatalf("the connection received %d calls", len(connection.calls))
			}
		})
	}
}

func TestGetDrainsChainedCallbacksBeforeReachingTheConnection(t *testing.T) {
	connection := &fakeConnection{}
	b := newTestBuilder(connection).Where("status", "=", "open")
	b.BeforeQuery(func(q *query.Builder) {
		q.BeforeQuery(func(next *query.Builder) {
			next.Wheres[0].Operator = "not an operator"
		})
	})

	_, err := b.Get(context.Background(), grant())
	if !errors.Is(err, query.ErrInvalidOperator) {
		t.Fatalf("Get() error = %v, want ErrInvalidOperator", err)
	}
	if len(connection.calls) != 0 {
		t.Fatalf("the connection received %d calls", len(connection.calls))
	}
}

// TestNoStatementReachesTheDatabaseWithoutATenant covers every door rather than
// one of them.
//
// A Grant nobody issued is the zero value, and the zero value carries no
// tenant. Every method that executes has to refuse it, and refuse it BEFORE the
// connection is touched -- a statement that reached the driver and was rejected
// there would already have been logged, planned and, on the wrong engine, run.
func TestNoStatementReachesTheDatabaseWithoutATenant(t *testing.T) {
	ctx := context.Background()

	doors := map[string]func(*query.Builder, auth.Grant) error{
		"Get": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Get(ctx, g)
			return err
		},
		"First": func(b *query.Builder, g auth.Grant) error {
			_, err := b.First(ctx, g)
			return err
		},
		"Find": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Find(ctx, g, 1)
			return err
		},
		"FirstOrFail": func(b *query.Builder, g auth.Grant) error {
			_, err := b.FirstOrFail(ctx, g, nil)
			return err
		},
		"Sole": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Sole(ctx, g)
			return err
		},
		"Value": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Value(ctx, g, "email")
			return err
		},
		"Pluck": func(b *query.Builder, g auth.Grant) error {
			_, _, err := b.Pluck(ctx, g, "email")
			return err
		},
		"Implode": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Implode(ctx, g, "email", ", ")
			return err
		},
		"Exists": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Exists(ctx, g)
			return err
		},
		"Count": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Count(ctx, g)
			return err
		},
		"Sum": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Sum(ctx, g, "total")
			return err
		},
		"GetCountForPagination": func(b *query.Builder, g auth.Grant) error {
			_, err := b.GetCountForPagination(ctx, g)
			return err
		},
		"Paginate": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Paginate(ctx, g, 15, 1, nil, pagination.Options{})
			return err
		},
		"SimplePaginate": func(b *query.Builder, g auth.Grant) error {
			_, err := b.SimplePaginate(ctx, g, 15, 1, nil, pagination.Options{})
			return err
		},
		"CursorPaginate": func(b *query.Builder, g auth.Grant) error {
			_, err := b.OrderBy("id").CursorPaginate(ctx, g, 15, nil, nil, signedOptions())
			return err
		},
		"Chunk": func(b *query.Builder, g auth.Grant) error {
			_, err := b.OrderBy("id").Chunk(ctx, g, 100, func([]query.Record, int) bool { return true })
			return err
		},
		"ChunkById": func(b *query.Builder, g auth.Grant) error {
			_, err := b.ChunkById(ctx, g, 100, func([]query.Record, int) bool { return true }, "", "")
			return err
		},
		"Each": func(b *query.Builder, g auth.Grant) error {
			_, err := b.OrderBy("id").Each(ctx, g, func(query.Record, int) bool { return true }, 100)
			return err
		},
		"Lazy": func(b *query.Builder, g auth.Grant) error {
			for _, err := range b.OrderBy("id").Lazy(ctx, g, 100) {
				return err
			}
			return nil
		},
		"LazyById": func(b *query.Builder, g auth.Grant) error {
			for _, err := range b.LazyById(ctx, g, 100, "", "") {
				return err
			}
			return nil
		},
		"Cursor": func(b *query.Builder, g auth.Grant) error {
			for _, err := range b.Cursor(ctx, g) {
				return err
			}
			return nil
		},
		"Insert": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Insert(ctx, g, map[string]any{"email": "a@b.c"})
			return err
		},
		"InsertOrIgnore": func(b *query.Builder, g auth.Grant) error {
			_, err := b.InsertOrIgnore(ctx, g, map[string]any{"email": "a@b.c"})
			return err
		},
		"InsertGetID": func(b *query.Builder, g auth.Grant) error {
			_, err := b.InsertGetID(ctx, g, map[string]any{"email": "a@b.c"}, "")
			return err
		},
		"InsertUsing": func(b *query.Builder, g auth.Grant) error {
			_, err := b.InsertUsing(ctx, g, []any{"email"}, "select email from staff")
			return err
		},
		"Update": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Update(ctx, g, map[string]any{"email": "a@b.c"})
			return err
		},
		"UpdateOrInsert": func(b *query.Builder, g auth.Grant) error {
			_, err := b.UpdateOrInsert(ctx, g, map[string]any{"id": 1}, map[string]any{"email": "a@b.c"})
			return err
		},
		"Upsert": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Upsert(ctx, g, []map[string]any{{"id": 1}}, []string{"id"}, nil)
			return err
		},
		"Increment": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Increment(ctx, g, "visits", 1, nil)
			return err
		},
		"Decrement": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Decrement(ctx, g, "visits", 1, nil)
			return err
		},
		"Delete": func(b *query.Builder, g auth.Grant) error {
			_, err := b.Delete(ctx, g)
			return err
		},
		"Truncate": func(b *query.Builder, g auth.Grant) error {
			return b.Truncate(ctx, g)
		},
		"ToRawSQL": func(b *query.Builder, g auth.Grant) error {
			_, err := b.ToRawSQL(ctx, g)
			return err
		},
	}

	for name, door := range doors {
		t.Run(name, func(t *testing.T) {
			connection := &fakeConnection{}
			err := door(newTestBuilder(connection), auth.Grant{})

			if err == nil {
				t.Fatalf("%s ran with a grant nobody issued", name)
			}
			if !errors.Is(err, auth.ErrForbidden) {
				t.Fatalf("%s refused with %v, which is not an authorization error", name, err)
			}
			if len(connection.calls) != 0 {
				t.Fatalf("%s reached the connection %d time(s) before refusing: %+v", name, len(connection.calls), connection.calls)
			}
		})
	}
}

// TestTheStatementCarriesTheTenantOfTheGrant: the filter has to be in the SQL,
// not in the intention.
func TestTheStatementCarriesTheTenantOfTheGrant(t *testing.T) {
	connection := &fakeConnection{}
	if _, err := newTestBuilder(connection).Get(context.Background(), grant()); err != nil {
		t.Fatal(err)
	}

	sql := connection.lastSQL()
	if !strings.Contains(sql, `"users"."tenant_id" = ?`) {
		t.Fatalf("the statement does not filter by tenant:\n%s", sql)
	}
	bindings := connection.lastBindings()
	if len(bindings) != 1 || bindings[0] != "acme" {
		t.Fatalf("the tenant of the grant is not bound: %#v", bindings)
	}
}

// TestTheTenantFilterBindsTheWholeWhereClause is the bug this would have had if
// the clause were simply appended.
//
// AND binds tighter than OR, so `a or b and tenant_id = ?` filters b and leaves
// a wide open -- and the query still compiles, still runs, and returns another
// customer's rows for the first branch only, which is the hardest kind of leak
// to notice.
func TestTheTenantFilterBindsTheWholeWhereClause(t *testing.T) {
	connection := &fakeConnection{}
	builder := newTestBuilder(connection).Where("role", "admin").OrWhere("role", "owner")

	if _, err := builder.Get(context.Background(), grant()); err != nil {
		t.Fatal(err)
	}

	sql := connection.lastSQL()
	want := `select * from "users" where "users"."tenant_id" = ? and ("role" = ? or "role" = ?)`
	if sql != want {
		t.Fatalf("the tenant filter does not bind the whole clause.\nwant: %s\ngot:  %s", want, sql)
	}
	bindings := connection.lastBindings()
	if len(bindings) != 3 || bindings[0] != "acme" || bindings[1] != "admin" || bindings[2] != "owner" {
		t.Fatalf("the bindings are out of order for the compiled statement: %#v", bindings)
	}
}

// TestTheBuilderIsNotChangedByRunningIt: the tenant clause belongs to the
// statement, not to the query. Running the same builder twice must not stack
// the filter, and running it under a second Grant must not carry the first
// tenant.
func TestTheBuilderIsNotChangedByRunningIt(t *testing.T) {
	connection := &fakeConnection{}
	builder := newTestBuilder(connection)
	ctx := context.Background()

	if _, err := builder.Get(ctx, grant()); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Get(ctx, auth.SystemGrant("invoice.read", "globex")); err != nil {
		t.Fatal(err)
	}

	second := connection.calls[1]
	if strings.Count(second.sql, "tenant_id") != 1 {
		t.Fatalf("the second run stacked the tenant filter:\n%s", second.sql)
	}
	if len(second.bindings) != 1 || second.bindings[0] != "globex" {
		t.Fatalf("the second run carried the first grant's tenant: %#v", second.bindings)
	}
	if len(builder.Wheres) != 0 {
		t.Fatalf("running the query left %d where clause(s) on the builder", len(builder.Wheres))
	}
}

// TestBothHalvesOfAUnionAreScoped: a union compiles each of its queries in
// full, so filtering only the outer one returns every row of the other -- for
// every customer, through a statement that looks filtered.
func TestBothHalvesOfAUnionAreScoped(t *testing.T) {
	connection := &fakeConnection{}
	archived := query.NewBuilder(connection, &fakeGrammar{}, &fakeProcessor{}).
		From("archived_users").Where("role", "admin")

	builder := newTestBuilder(connection).Union(archived)
	if _, err := builder.Get(context.Background(), grant()); err != nil {
		t.Fatal(err)
	}

	sql := connection.lastSQL()
	if !strings.Contains(sql, `"archived_users"."tenant_id" = ?`) {
		t.Fatalf("the far side of the union runs unfiltered:\n%s", sql)
	}

	bindings := connection.lastBindings()
	if len(bindings) != 3 {
		t.Fatalf("the union bindings were not collected again: %#v", bindings)
	}
	if bindings[0] != "acme" || bindings[1] != "acme" || bindings[2] != "admin" {
		t.Fatalf("the bindings do not follow the compiled statement: %#v", bindings)
	}
}

func TestGetSelectsEveryColumnAndReturnsTheRows(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{
		{"id": int64(1), "email": "ana@example.com"},
		{"id": int64(2), "email": "bruno@example.com"},
	}}}

	rows, err := newTestBuilder(connection).Get(context.Background(), grant())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1]["email"] != "bruno@example.com" {
		t.Fatalf("the rows did not come back: %#v", rows)
	}
	if !strings.HasPrefix(connection.lastSQL(), "select * from") {
		t.Fatalf("get did not default to every column:\n%s", connection.lastSQL())
	}
}

func TestFirstLimitsToOneRowAndAnswersNilForNone(t *testing.T) {
	connection := &fakeConnection{}
	row, err := newTestBuilder(connection).First(context.Background(), grant(), "email")
	if err != nil {
		t.Fatal(err)
	}
	if row != nil {
		t.Fatalf("first answered %#v where the PHP answers null", row)
	}
	if !strings.Contains(connection.lastSQL(), "limit 1") {
		t.Fatalf("first did not limit the statement:\n%s", connection.lastSQL())
	}
	if !strings.Contains(connection.lastSQL(), `select "email"`) {
		t.Fatalf("first did not select the column it was given:\n%s", connection.lastSQL())
	}
}

func TestFindLooksTheRowUpById(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{{"id": int64(3)}}}}
	row, err := newTestBuilder(connection).Find(context.Background(), grant(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if row["id"] != int64(3) {
		t.Fatalf("find answered %#v", row)
	}
	if !strings.Contains(connection.lastSQL(), `"id" = ?`) {
		t.Fatalf("find did not filter by id:\n%s", connection.lastSQL())
	}
}

func TestFindOrRunsTheCallbackWhenTheRowIsNotThere(t *testing.T) {
	connection := &fakeConnection{}
	fallback := query.Record{"id": int64(0)}

	row, err := newTestBuilder(connection).FindOr(context.Background(), grant(), 9, nil, func() (query.Record, error) {
		return fallback, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if row["id"] != int64(0) {
		t.Fatalf("findOr did not fall back: %#v", row)
	}
}

func TestFirstOrFailReportsRecordNotFound(t *testing.T) {
	connection := &fakeConnection{}
	_, err := newTestBuilder(connection).FirstOrFail(context.Background(), grant(), nil)
	if !errors.Is(err, query.ErrRecordNotFound) {
		t.Fatalf("firstOrFail answered %v", err)
	}
}

func TestSoleRefusesNoneAndMoreThanOne(t *testing.T) {
	ctx := context.Background()

	none := &fakeConnection{}
	if _, err := newTestBuilder(none).Sole(ctx, grant()); !errors.Is(err, query.ErrRecordsNotFound) {
		t.Fatalf("sole over no rows answered %v", err)
	}

	many := &fakeConnection{results: [][]query.Record{{{"id": 1}, {"id": 2}}}}
	if _, err := newTestBuilder(many).Sole(ctx, grant()); !errors.Is(err, query.ErrMultipleRecordsFound) {
		t.Fatalf("sole over two rows answered %v", err)
	}
	if !strings.Contains(many.lastSQL(), "limit 2") {
		t.Fatalf("sole read more rows than it needs:\n%s", many.lastSQL())
	}
}

func TestValueReadsTheColumnOffTheFirstRow(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{{"email": "ana@example.com"}}}}
	value, err := newTestBuilder(connection).Value(context.Background(), grant(), "users.email")
	if err != nil {
		t.Fatal(err)
	}
	if value != "ana@example.com" {
		t.Fatalf("value answered %#v", value)
	}
}

func TestPluckAnswersTheListAndTheKeyedMap(t *testing.T) {
	rows := []query.Record{
		{"id": int64(1), "email": "ana@example.com"},
		{"id": int64(2), "email": "bruno@example.com"},
	}

	connection := &fakeConnection{results: [][]query.Record{rows}}
	values, keyed, err := newTestBuilder(connection).Pluck(context.Background(), grant(), "email")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0] != "ana@example.com" {
		t.Fatalf("pluck answered %#v", values)
	}
	if keyed != nil {
		t.Fatalf("pluck without a key answered a map: %#v", keyed)
	}

	connection = &fakeConnection{results: [][]query.Record{rows}}
	_, keyed, err = newTestBuilder(connection).Pluck(context.Background(), grant(), "email", "id")
	if err != nil {
		t.Fatal(err)
	}
	if keyed["2"] != "bruno@example.com" {
		t.Fatalf("pluck with a key answered %#v", keyed)
	}
}

func TestImplodeJoinsThePluckedValues(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{{"email": "a@b.c"}, {"email": "d@e.f"}}}}
	joined, err := newTestBuilder(connection).Implode(context.Background(), grant(), "email", ", ")
	if err != nil {
		t.Fatal(err)
	}
	if joined != "a@b.c, d@e.f" {
		t.Fatalf("implode answered %q", joined)
	}
}

func TestExistsReadsTheExistsColumn(t *testing.T) {
	ctx := context.Background()

	yes := &fakeConnection{results: [][]query.Record{{{"exists": int64(1)}}}}
	got, err := newTestBuilder(yes).Exists(ctx, grant())
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("exists answered false for a row saying 1")
	}
	if !strings.HasPrefix(yes.lastSQL(), "select exists(") {
		t.Fatalf("exists did not compile through the grammar:\n%s", yes.lastSQL())
	}
	if !strings.Contains(yes.lastSQL(), "tenant_id") {
		t.Fatalf("exists asked about rows outside the tenant:\n%s", yes.lastSQL())
	}

	no := &fakeConnection{}
	got, err = newTestBuilder(no).DoesntExist(ctx, grant())
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("doesntExist answered false over no rows")
	}
}

func TestExistsOrRunsTheCallbackOnlyWhenNothingIsThere(t *testing.T) {
	ctx := context.Background()
	ran := false

	connection := &fakeConnection{}
	existed, err := newTestBuilder(connection).ExistsOr(ctx, grant(), func() error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if existed || !ran {
		t.Fatalf("existsOr answered existed=%v, ran=%v", existed, ran)
	}
}

func TestCountAsksForAnAggregateAndDropsTheOrdering(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{{"aggregate": int64(12)}}}}

	total, err := newTestBuilder(connection).OrderBy("email").Count(context.Background(), grant())
	if err != nil {
		t.Fatal(err)
	}
	if total != 12 {
		t.Fatalf("count answered %d", total)
	}

	sql := connection.lastSQL()
	if !strings.HasPrefix(sql, `select count(*) as aggregate`) {
		t.Fatalf("count did not compile as an aggregate:\n%s", sql)
	}
	if strings.Contains(sql, "order by") {
		t.Fatalf("the aggregate kept an ordering nobody can use:\n%s", sql)
	}
}

func TestAggregateAnswersNilOverNoRows(t *testing.T) {
	connection := &fakeConnection{}
	value, err := newTestBuilder(connection).Max(context.Background(), grant(), "total")
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		t.Fatalf("max over no rows answered %#v", value)
	}
}

func TestSumAnswersZeroRatherThanNil(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{{"aggregate": nil}}}}
	value, err := newTestBuilder(connection).Sum(context.Background(), grant(), "total")
	if err != nil {
		t.Fatal(err)
	}
	if value != 0 {
		t.Fatalf("sum over no rows answered %#v", value)
	}
}

func TestNumericAggregateAnswersANumber(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{{"aggregate": []byte("41.5")}}}}
	value, err := newTestBuilder(connection).NumericAggregate(context.Background(), grant(), "avg", "total")
	if err != nil {
		t.Fatal(err)
	}
	if value != 41.5 {
		t.Fatalf("numericAggregate answered %v", value)
	}
}

func TestGetCountForPaginationDropsTheLimitAndTheOffset(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{{"aggregate": int64(30)}}}}

	total, err := newTestBuilder(connection).ForPage(3, 10).GetCountForPagination(context.Background(), grant())
	if err != nil {
		t.Fatal(err)
	}
	if total != 30 {
		t.Fatalf("the count for pagination answered %d", total)
	}
	if strings.Contains(connection.lastSQL(), "limit") || strings.Contains(connection.lastSQL(), "offset") {
		t.Fatalf("the count query counted one page:\n%s", connection.lastSQL())
	}
}

func TestGetCountForPaginationWrapsAGroupedQuery(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{{"aggregate": int64(4)}}}}

	total, err := newTestBuilder(connection).GroupBy("role").GetCountForPagination(context.Background(), grant())
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Fatalf("the grouped count answered %d", total)
	}

	sql := connection.lastSQL()
	if !strings.Contains(sql, `as "aggregate_table"`) {
		t.Fatalf("a grouped query was counted without a derived table:\n%s", sql)
	}
	if !strings.Contains(sql, "tenant_id") {
		t.Fatalf("the derived table was not scoped by tenant:\n%s", sql)
	}
	if bindings := connection.lastBindings(); len(bindings) != 1 || bindings[0] != "acme" {
		t.Fatalf("the inner bindings did not survive the wrapping: %#v", bindings)
	}
}

func TestPaginateCountsOnceAndReadsOnePage(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{
		{{"aggregate": int64(42)}},
		{{"id": int64(11)}, {"id": int64(12)}},
	}}

	page, err := newTestBuilder(connection).Paginate(context.Background(), grant(), 2, 3, nil, pagination.Options{Path: "/users"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total() != 42 || page.CurrentPage() != 3 || page.LastPage() != 21 {
		t.Fatalf("the paginator says total=%d page=%d last=%d", page.Total(), page.CurrentPage(), page.LastPage())
	}
	if len(page.Items()) != 2 {
		t.Fatalf("the page holds %d rows", len(page.Items()))
	}
	if !strings.Contains(connection.lastSQL(), "limit 2 offset 4") {
		t.Fatalf("the page query did not ask for page three:\n%s", connection.lastSQL())
	}
}

func TestPaginateWithAKnownTotalSkipsTheCountQuery(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{{"id": int64(1)}}}}

	page, err := newTestBuilder(connection).Paginate(context.Background(), grant(), 15, 1, nil, pagination.Options{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total() != 1 {
		t.Fatalf("the paginator ignored the total it was given: %d", page.Total())
	}
	if len(connection.calls) != 1 {
		t.Fatalf("the count query ran anyway: %d statements", len(connection.calls))
	}
}

func TestSimplePaginateReadsOneRowMoreThanThePage(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{
		{"id": int64(1)}, {"id": int64(2)}, {"id": int64(3)},
	}}}

	page, err := newTestBuilder(connection).SimplePaginate(context.Background(), grant(), 2, 1, nil, pagination.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(connection.lastSQL(), "limit 3") {
		t.Fatalf("simplePaginate did not read the probe row:\n%s", connection.lastSQL())
	}
	if len(page.Items()) != 2 {
		t.Fatalf("the probe row was handed to the reader: %d rows", len(page.Items()))
	}
	if !page.HasMorePages() {
		t.Fatal("the probe row did not answer the question it exists for")
	}
	if len(connection.calls) != 1 {
		t.Fatalf("simplePaginate counted: %d statements", len(connection.calls))
	}
}

func TestCursorPaginateWalksFromTheBoundary(t *testing.T) {
	connection := &fakeConnection{results: [][]query.Record{{
		{"id": int64(6)}, {"id": int64(7)},
	}}}
	cursor := pagination.NewCursor(map[string]string{"id": "5"}, true)

	page, err := newTestBuilder(connection).
		OrderBy("id").
		CursorPaginate(context.Background(), grant(), 1, &cursor, nil, signedOptions())
	if err != nil {
		t.Fatal(err)
	}

	sql := connection.lastSQL()
	if !strings.Contains(sql, `"id" > ?`) {
		t.Fatalf("the cursor did not become a boundary:\n%s", sql)
	}
	if !strings.Contains(sql, "order by") || !strings.Contains(sql, "limit 2") {
		t.Fatalf("the cursor page was not ordered and probed:\n%s", sql)
	}
	bindings := connection.lastBindings()
	if len(bindings) != 2 || bindings[0] != "acme" || bindings[1] != "5" {
		t.Fatalf("the boundary value is not bound: %#v", bindings)
	}
	if len(page.Items()) != 1 || !page.HasMorePages() {
		t.Fatalf("the cursor page holds %d rows, more=%v", len(page.Items()), page.HasMorePages())
	}
	if page.NextCursor() == nil {
		t.Fatal("the page has no cursor to walk on with")
	}
}

func TestCursorPaginateRefusesAnUnorderedQuery(t *testing.T) {
	connection := &fakeConnection{}
	_, err := newTestBuilder(connection).CursorPaginate(context.Background(), grant(), 15, nil, nil, signedOptions())
	if err == nil {
		t.Fatal("a keyset walk with no ordering was allowed")
	}
	if len(connection.calls) != 0 {
		t.Fatal("the unordered walk reached the connection")
	}
}

func TestToRawSQLWritesTheBindingsIn(t *testing.T) {
	connection := &fakeConnection{}
	sql, err := newTestBuilder(connection).Where("email", "ana@example.com").ToRawSQL(context.Background(), grant())
	if err != nil {
		t.Fatal(err)
	}
	want := `select * from "users" where "users"."tenant_id" = 'acme' and ("email" = 'ana@example.com')`
	if sql != want {
		t.Fatalf("the raw statement reads\n%s\nwant\n%s", sql, want)
	}
	if len(connection.calls) != 0 {
		t.Fatal("toRawSql ran the statement it was asked to print")
	}
}

func TestToRawSQLLeavesAQuestionMarkInsideALiteralAlone(t *testing.T) {
	connection := &fakeConnection{}
	sql, err := newTestBuilder(connection).WhereRaw("label = 'why?'").Where("id", 1).
		ToRawSQL(context.Background(), grant())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, `label = 'why?'`) {
		t.Fatalf("a question mark inside a literal was substituted:\n%s", sql)
	}
	if !strings.Contains(sql, `"id" = 1`) {
		t.Fatalf("the binding after the literal landed in the wrong place:\n%s", sql)
	}
}

func TestCleanBindingsDropsExpressions(t *testing.T) {
	builder := newTestBuilder(&fakeConnection{})
	got := builder.CleanBindings([]any{1, query.Raw("now()"), "two"})
	if len(got) != 2 || got[0] != 1 || got[1] != "two" {
		t.Fatalf("cleanBindings answered %#v", got)
	}
}

func TestACancelledContextStopsTheStatement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	connection := &fakeConnection{}
	if _, err := newTestBuilder(connection).Get(ctx, grant()); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled context answered %v", err)
	}
	if len(connection.calls) != 0 {
		t.Fatal("the statement was issued on a cancelled context")
	}
}
