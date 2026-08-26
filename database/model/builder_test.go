package model

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/pagination"
)

// fakeRelation stands in for model/relations while this slice is written. It
// is the whole of what the builder asks a relation for.
type fakeRelation struct {
	table   string
	foreign string
	local   string
	matched map[any]any
}

func (r *fakeRelation) GetRelationExistenceQuery(parent *query.Builder, columns any) *query.Builder {
	sub := query.NewBuilder(parent.Connection, parent.Grammar, parent.Processor)
	sub.From(r.table).Select(columns).WhereColumn(r.foreign, "=", r.local)
	return sub
}

func (r *fakeRelation) Match(g auth.Grant, keys []any, constraints func(*query.Builder)) (map[any]any, error) {
	return r.matched, nil
}

func withPosts(model *Model[user], relation *fakeRelation) *Model[user] {
	model.RelationResolvers = map[string]func(*Model[user]) Relation{
		"posts": func(*Model[user]) Relation { return relation },
	}
	return model
}

func TestGetScopesEveryReadByTheTenant(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(query.Record{"id": int64(1), "name": "Ada"})

	models, err := model.NewQuery().Where("name", "=", "Ada").Get(context.Background(), grant())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(models) != 1 || models[0].Entity.Name != "Ada" {
		t.Fatalf("Get returned %d models", len(models))
	}

	last := conn.last()
	if !strings.Contains(last.SQL, `"users"."tenant_id" = ?`) {
		t.Errorf("SQL = %q, and a read is scoped by tenant exactly like a write (RULE 17)", last.SQL)
	}
	if last.Bindings[len(last.Bindings)-1] != "acme" {
		t.Errorf("bindings = %v, want the tenant last", last.Bindings)
	}
}

func TestTheTenantFilterIsNotSwallowedByAnOr(t *testing.T) {
	model, conn := newUserModel()
	conn.queue()

	if _, err := model.NewQuery().Where("name", "=", "Ada").OrWhere("email", "=", "a@b").Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := conn.last().SQL
	want := `where ("name" = ? or "email" = ?) and "users"."tenant_id" = ?`
	if !strings.Contains(sql, want) {
		t.Fatalf("SQL = %q, want %q.\n`a or b and tenant = ?` reads as `a or (b and tenant = ?)`, so every row matching a comes back whoever it belongs to", sql, want)
	}
}

func TestTheScopesAndTheTenantGoOnExactlyOnce(t *testing.T) {
	model, conn := newUserModel()
	model.SoftDeletes = true
	conn.queue()

	if _, err := model.NewQuery().Where("name", "=", "Ada").Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := conn.last().SQL
	if strings.Count(sql, "tenant_id") != 1 || strings.Count(sql, "deleted_at") != 1 {
		t.Fatalf("SQL = %q: every method that runs prepares, and they call each other -- so the filters landed twice", sql)
	}
	if len(conn.last().Bindings) != 2 {
		t.Errorf("bindings = %v, want one for the where and one for the tenant", conn.last().Bindings)
	}
}

func TestEveryReadRefusesAGrantWithNoTenant(t *testing.T) {
	zero := auth.Grant{}

	reads := map[string]func(*Model[user]) error{
		"All": func(m *Model[user]) error {
			_, err := m.All(context.Background(), zero)
			return err
		},
		"Get": func(m *Model[user]) error {
			_, err := m.NewQuery().Get(context.Background(), zero)
			return err
		},
		"First": func(m *Model[user]) error {
			_, err := m.NewQuery().First(context.Background(), zero)
			return err
		},
		"Find": func(m *Model[user]) error {
			_, err := m.NewQuery().Find(context.Background(), zero, 1)
			return err
		},
		"Count": func(m *Model[user]) error {
			_, err := m.NewQuery().Count(context.Background(), zero)
			return err
		},
		"Pluck": func(m *Model[user]) error {
			_, err := m.NewQuery().Pluck(context.Background(), zero, "name")
			return err
		},
		"Value": func(m *Model[user]) error {
			_, err := m.NewQuery().Value(context.Background(), zero, "name")
			return err
		},
		"Paginate": func(m *Model[user]) error {
			_, err := m.NewQuery().Paginate(context.Background(), zero, 10, 1, pagination.Options{})
			return err
		},
		"SimplePaginate": func(m *Model[user]) error {
			_, err := m.NewQuery().SimplePaginate(context.Background(), zero, 10, 1, pagination.Options{})
			return err
		},
		"CursorPaginate": func(m *Model[user]) error {
			_, err := m.NewQuery().CursorPaginate(context.Background(), zero, 10, nil, signedOptions())
			return err
		},
		"Chunk": func(m *Model[user]) error {
			return m.NewQuery().Chunk(context.Background(), zero, 10, func(Collection[user], int) (bool, error) { return true, nil })
		},
		"FromQuery": func(m *Model[user]) error {
			_, err := m.NewQuery().FromQuery(context.Background(), zero, "select 1", nil)
			return err
		},
		"Insert": func(m *Model[user]) error {
			_, err := m.NewQuery().Insert(context.Background(), zero, map[string]any{"name": "Ada"})
			return err
		},
		"Update": func(m *Model[user]) error {
			_, err := m.NewQuery().Update(context.Background(), zero, map[string]any{"name": "Ada"})
			return err
		},
		"Delete": func(m *Model[user]) error {
			_, err := m.NewQuery().Delete(context.Background(), zero)
			return err
		},
		"ForceDelete": func(m *Model[user]) error {
			_, err := m.NewQuery().ForceDelete(context.Background(), zero)
			return err
		},
	}

	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			model, conn := newUserModel()
			conn.queue(query.Record{"id": int64(1)})

			if err := read(model); !errors.Is(err, ErrNoTenant) {
				t.Fatalf("%s with the zero Grant = %v, want ErrNoTenant", name, err)
			}
			if len(conn.sqls()) != 0 {
				t.Errorf("%s ran %v with no grant behind it", name, conn.sqls())
			}
		})
	}
}

func TestFirstReturnsNothingRatherThanAnError(t *testing.T) {
	model, conn := newUserModel()
	conn.queue()

	found, err := model.NewQuery().First(context.Background(), grant())
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if found != nil {
		t.Error("First invented a model out of an empty result")
	}
}

func TestFirstOrFailAnswersTheExceptionAsAnError(t *testing.T) {
	model, conn := newUserModel()
	conn.queue()

	_, err := model.NewQuery().FirstOrFail(context.Background(), grant())
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("FirstOrFail error = %v, want ErrModelNotFound", err)
	}
	if !strings.Contains(err.Error(), "users") {
		t.Errorf("error = %q, and it has to name the table", err)
	}
}

func TestFindOrFailNamesTheIdItLookedFor(t *testing.T) {
	model, conn := newUserModel()
	conn.queue()

	_, err := model.NewQuery().FindOrFail(context.Background(), grant(), 7)
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("FindOrFail error = %v, want ErrModelNotFound", err)
	}
	if !strings.Contains(err.Error(), "7") {
		t.Errorf("error = %q, want the id in it", err)
	}
}

func TestSoleRefusesASecondRow(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(query.Record{"id": int64(1)}, query.Record{"id": int64(2)})

	if _, err := model.NewQuery().Sole(context.Background(), grant()); !errors.Is(err, ErrMultipleRecordsFound) {
		t.Fatalf("Sole error = %v, want ErrMultipleRecordsFound", err)
	}
}

func TestFirstOrCreateReadsBeforeItWrites(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(query.Record{"id": int64(3), "name": "Ada"})

	found, err := model.NewQuery().FirstOrCreate(context.Background(), grant(), map[string]any{"name": "Ada"}, nil)
	if err != nil {
		t.Fatalf("FirstOrCreate: %v", err)
	}
	if found.Entity.ID != 3 {
		t.Errorf("id = %d, want the row that was already there", found.Entity.ID)
	}
	for _, sql := range conn.sqls() {
		if strings.HasPrefix(sql, "insert") {
			t.Errorf("FirstOrCreate inserted although the row was there: %q", sql)
		}
	}
}

func TestFirstOrCreateInsertsWhenThereIsNothing(t *testing.T) {
	model, conn := newUserModel()
	conn.queue()

	created, err := model.NewQuery().FirstOrCreate(context.Background(), grant(), map[string]any{"name": "Ada"}, map[string]any{"email": "ada@example.com"})
	if err != nil {
		t.Fatalf("FirstOrCreate: %v", err)
	}
	if created.Entity.Name != "Ada" || created.Entity.Email != "ada@example.com" {
		t.Errorf("created = %+v, want the attributes and the values merged", created.Entity)
	}
	if !created.WasRecentlyCreated {
		t.Error("the new model does not report that it was just created")
	}
}

func TestUpdateOrCreateUpdatesTheRowItFound(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(query.Record{"id": int64(3), "name": "Ada", "email": "old@example.com"})

	updated, err := model.NewQuery().UpdateOrCreate(context.Background(), grant(),
		map[string]any{"name": "Ada"},
		map[string]any{"email": "new@example.com"})
	if err != nil {
		t.Fatalf("UpdateOrCreate: %v", err)
	}
	if updated.Entity.Email != "new@example.com" {
		t.Errorf("email = %q, want the new one", updated.Entity.Email)
	}
	if !strings.HasPrefix(conn.last().SQL, `update "users"`) {
		t.Errorf("last statement = %q, want an update", conn.last().SQL)
	}
}

func TestWhereHasCompilesAnExistsSubquery(t *testing.T) {
	model, conn := newUserModel()
	withPosts(model, &fakeRelation{table: "posts", foreign: "posts.user_id", local: "users.id"})
	conn.queue()

	_, err := model.NewQuery().WhereHas("posts", func(sub *query.Builder) {
		sub.Where("published", "=", true)
	}).Get(context.Background(), grant())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := conn.last().SQL
	// The subquery carries a tenant of its own. Without it the exists asked
	// whether ANY tenant had a matching post, which selected this tenant's users
	// by another tenant's rows -- the shape this test used to assert.
	if !strings.Contains(sql, `exists (select * from "posts" where "posts"."tenant_id" = ? and ("posts"."user_id" = "users"."id" and "published" = ?))`) {
		t.Fatalf("SQL = %q, want the correlated exists whereHas compiles to, scoped", sql)
	}
	if got := conn.last().Bindings[0]; got != "acme" {
		t.Errorf("bindings = %v, want the subquery's tenant first", conn.last().Bindings)
	}
	if got := conn.last().Bindings[1]; got != true {
		t.Errorf("bindings = %v, want the subquery's own binding after its tenant", conn.last().Bindings)
	}
}

func TestHasWithACountCompilesTheSubqueryAsAComparison(t *testing.T) {
	model, conn := newUserModel()
	withPosts(model, &fakeRelation{table: "posts", foreign: "posts.user_id", local: "users.id"})
	conn.queue()

	if _, err := model.NewQuery().Has("posts", ">=", 3, "and", nil).Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}

	last := conn.last()
	if !strings.Contains(last.SQL, `(select count(*) from "posts" where "posts"."tenant_id" = ?`) {
		t.Fatalf("SQL = %q, want the count subquery scoped by its own tenant", last.SQL)
	}
	if !strings.Contains(last.SQL, `>= ?`) {
		t.Fatalf("SQL = %q, want the count subquery compared against a bound number", last.SQL)
	}
	// The subquery's tenant, then the number it is compared against, then the
	// outer tenant: the clause carries its own bindings, so the list is rebuilt
	// in the order the statement reads them.
	if want := []any{"acme", 3, "acme"}; !slices.Equal(last.Bindings, want) {
		t.Errorf("bindings = %v, want %v", last.Bindings, want)
	}
}

func TestDoesntHaveCompilesANotExists(t *testing.T) {
	model, conn := newUserModel()
	withPosts(model, &fakeRelation{table: "posts", foreign: "posts.user_id", local: "users.id"})
	conn.queue()

	if _, err := model.NewQuery().WhereDoesntHave("posts", nil).Get(context.Background(), grant()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(conn.last().SQL, "not exists (") {
		t.Fatalf("SQL = %q, want a not exists", conn.last().SQL)
	}
}

func TestAnUnknownRelationIsReportedByTheFirstMethodThatRuns(t *testing.T) {
	model, _ := newUserModel()

	_, err := model.NewQuery().WhereHas("posts", nil).Get(context.Background(), grant())
	if !errors.Is(err, ErrRelationNotFound) {
		t.Fatalf("Get error = %v, want ErrRelationNotFound held from the build and reported here", err)
	}
}

func TestWithCountAddsTheAliasedSubselect(t *testing.T) {
	model, conn := newUserModel()
	withPosts(model, &fakeRelation{table: "posts", foreign: "posts.user_id", local: "users.id"})
	conn.queue(query.Record{"id": int64(1), "posts_count": int64(4)})

	models, err := model.NewQuery().WithCount("posts").Get(context.Background(), grant())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := conn.last().SQL
	if !strings.Contains(sql, `(select count(*) from "posts" where "posts"."tenant_id" = ? and ("posts"."user_id" = "users"."id")) as "posts_count"`) {
		t.Fatalf("SQL = %q, want the aggregate subselect aliased posts_count, scoped", sql)
	}
	if !strings.Contains(sql, `"users".*`) {
		t.Errorf("SQL = %q: withAggregate selects the table's own columns before it adds the subselect", sql)
	}
	if got := models[0].GetAttribute("posts_count"); got != int64(4) {
		t.Errorf("posts_count = %v, want 4 read back as a raw attribute", got)
	}
}

func TestWithLoadsTheRelationOntoEveryModel(t *testing.T) {
	model, conn := newUserModel()
	withPosts(model, &fakeRelation{
		table: "posts", foreign: "posts.user_id", local: "users.id",
		matched: map[any]any{int64(1): []string{"first"}, int64(2): []string{"second"}},
	})
	conn.queue(query.Record{"id": int64(1)}, query.Record{"id": int64(2)})

	models, err := model.NewQuery().With("posts").Get(context.Background(), grant())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	loaded, ok := models[0].GetRelation("posts")
	if !ok {
		t.Fatal("the relation was not set on the model")
	}
	if loaded.([]string)[0] != "first" {
		t.Errorf("relation = %v, want the rows matched to this parent", loaded)
	}
}

func TestChunkWalksThePagesAndStops(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(query.Record{"id": int64(1)}, query.Record{"id": int64(2)})
	conn.queue(query.Record{"id": int64(3)})

	var seen []int64
	err := model.NewQuery().Chunk(context.Background(), grant(), 2, func(models Collection[user], page int) (bool, error) {
		for _, m := range models {
			seen = append(seen, m.Entity.ID)
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("saw %v, want three rows over two chunks", seen)
	}
	if !strings.Contains(conn.sqls()[0], "order by") {
		t.Errorf("SQL = %q: chunking without an order walks a set the engine may return differently each time", conn.sqls()[0])
	}
}

func TestChunkStopsWhenTheCallbackSaysSo(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(query.Record{"id": int64(1)}, query.Record{"id": int64(2)})
	conn.queue(query.Record{"id": int64(3)}, query.Record{"id": int64(4)})

	pages := 0
	err := model.NewQuery().Chunk(context.Background(), grant(), 2, func(Collection[user], int) (bool, error) {
		pages++
		return false, nil
	})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if pages != 1 {
		t.Errorf("pages = %d, want the walk to stop on the first false", pages)
	}
}

func TestPaginateCountsThenReadsThePage(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(query.Record{"aggregate": int64(7)})
	conn.queue(query.Record{"id": int64(1)}, query.Record{"id": int64(2)})

	page, err := model.NewQuery().Paginate(context.Background(), grant(), 2, 2, pagination.Options{Path: "/users"})
	if err != nil {
		t.Fatalf("Paginate: %v", err)
	}
	if page.Total() != 7 {
		t.Errorf("Total() = %d, want 7", page.Total())
	}
	if page.LastPage() != 4 {
		t.Errorf("LastPage() = %d, want 4", page.LastPage())
	}
	if len(page.Items()) != 2 {
		t.Errorf("Items() = %d rows, want 2", len(page.Items()))
	}

	sqls := conn.sqls()
	if !strings.Contains(sqls[0], "count(*) as aggregate") {
		t.Errorf("first statement = %q, want the count", sqls[0])
	}
	if strings.Contains(sqls[0], "limit") {
		t.Errorf("count = %q: a count with the page's limit on it counts the page", sqls[0])
	}
	if !strings.Contains(sqls[1], "limit 2 offset 2") {
		t.Errorf("page query = %q, want the second page of two", sqls[1])
	}
}

func TestSimplePaginateReadsOneMoreRowThanThePage(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(query.Record{"id": int64(1)}, query.Record{"id": int64(2)}, query.Record{"id": int64(3)})

	page, err := model.NewQuery().SimplePaginate(context.Background(), grant(), 2, 1, pagination.Options{})
	if err != nil {
		t.Fatalf("SimplePaginate: %v", err)
	}
	if !strings.Contains(conn.last().SQL, "limit 3") {
		t.Errorf("SQL = %q, want perPage+1 -- the extra row is how the next page is answered without a count", conn.last().SQL)
	}
	if len(page.Items()) != 2 || !page.HasMorePages() {
		t.Errorf("page holds %d rows, more = %v", len(page.Items()), page.HasMorePages())
	}
}

func TestCursorPaginateComparesAgainstTheBoundary(t *testing.T) {
	model, conn := newUserModel()
	conn.queue(query.Record{"id": int64(4)}, query.Record{"id": int64(5)})

	cursor := pagination.NewCursor(map[string]string{"users.id": "3"}, true)
	page, err := model.NewQuery().CursorPaginate(context.Background(), grant(), 1, &cursor, signedOptions())
	if err != nil {
		t.Fatalf("CursorPaginate: %v", err)
	}

	sql := conn.last().SQL
	if !strings.Contains(sql, `"users"."id" > ?`) {
		t.Fatalf("SQL = %q, want the boundary comparison", sql)
	}
	if len(page.Items()) != 1 || page.NextCursor() == nil {
		t.Errorf("page holds %d rows, next = %v", len(page.Items()), page.NextCursor())
	}
}

func TestUpsertSortsTheColumnsItCompilesAndBinds(t *testing.T) {
	model, conn := newUserModel()

	if _, err := model.NewQuery().Upsert(context.Background(), grant(),
		[]map[string]any{{"name": "Ada", "email": "ada@example.com"}},
		[]string{"email"}, nil); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	last := conn.last()
	if !strings.Contains(last.SQL, `("email", "name", "tenant_id", "updated_at")`) {
		t.Fatalf("SQL = %q, want the columns sorted, so the bindings land in an order the caller's map cannot change", last.SQL)
	}
	if last.Bindings[0] != "ada@example.com" || last.Bindings[1] != "Ada" {
		t.Errorf("bindings = %v, want them in the same order as the columns", last.Bindings)
	}
}

func TestIncrementCompilesTheColumnAgainstItself(t *testing.T) {
	model, conn := newUserModel()

	if _, err := model.NewQuery().Increment(context.Background(), grant(), "logins", 2, nil); err != nil {
		t.Fatalf("Increment: %v", err)
	}
	if !strings.Contains(conn.last().SQL, `"logins" + 2`) {
		t.Errorf("SQL = %q, want the column incremented in place", conn.last().SQL)
	}
}

func TestIncrementRefusesAnAmountThatIsNotANumber(t *testing.T) {
	model, conn := newUserModel()

	_, err := model.NewQuery().Increment(context.Background(), grant(), "logins", "1; drop table users", nil)
	if err == nil {
		t.Fatal("Increment took a string amount, and the amount is compiled into the statement rather than bound")
	}
	if len(conn.sqls()) != 0 {
		t.Errorf("it ran %v", conn.sqls())
	}
}

func TestEagerLoadConstraintsNarrowTheSubqueryAndNotTheOuterOne(t *testing.T) {
	model, conn := newUserModel()
	withPosts(model, &fakeRelation{table: "posts", foreign: "posts.user_id", local: "users.id"})
	conn.queue()

	_, err := model.NewQuery().
		WithConstraints("posts", func(sub *query.Builder) { sub.Where("published", "=", true) }).
		WithCount("posts").
		Get(context.Background(), grant())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	sql := conn.last().SQL
	if !strings.Contains(sql, `from "posts" where "posts"."tenant_id" = ? and ("posts"."user_id" = "users"."id" and "published" = ?)`) {
		t.Fatalf("SQL = %q, want the constraint inside the subquery, under the subquery's own tenant", sql)
	}
	if strings.Contains(sql, `from "users" where "published"`) {
		t.Errorf("SQL = %q: the constraint narrowed the outer query, which is a filter the caller never asked for", sql)
	}
}

func TestFillAndInsertWritesTheColumnsASaveWouldHave(t *testing.T) {
	model, conn := newUserModel()

	ok, err := model.NewQuery().FillAndInsert(context.Background(), grant(), []map[string]any{
		{"name": "Ada"},
		{"name": "Grace"},
	})
	if err != nil || !ok {
		t.Fatalf("FillAndInsert = %v, %v", ok, err)
	}

	last := conn.last()
	if !strings.Contains(last.SQL, `"created_at"`) || !strings.Contains(last.SQL, `"tenant_id"`) {
		t.Fatalf("SQL = %q, want the timestamps and the tenant a save would have written", last.SQL)
	}
	if strings.Count(last.SQL, "), (") != 1 {
		t.Errorf("SQL = %q, want both rows in one statement", last.SQL)
	}
}

func TestToBaseHandsOutAQueryThatIsAlreadyScoped(t *testing.T) {
	model, _ := newUserModel()

	base, err := model.NewQuery().ToBase(context.Background(), grant())
	if err != nil {
		t.Fatalf("ToBase: %v", err)
	}
	if !strings.Contains(base.ToSQL(), `"users"."tenant_id" = ?`) {
		t.Errorf("SQL = %q: a base builder handed out unscoped is a query somebody will run", base.ToSQL())
	}
}
