package grammars_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
)

// The point of every test in this file is the same one: the same builder, four
// grammars, four literal strings.
//
// "Swapping the driver duplicates nothing" is a claim about this package, and
// until the four expected strings sit side by side in one table nobody can see
// whether it is true. A diff that changes one dialect and quietly changes
// another shows up here as a failing literal, which is the only way that class
// of mistake gets caught -- a compiler cannot tell one SQL string from another.

// dialects are the four grammars, in the order they are written out.
func dialects() []struct {
	name    string
	grammar func() query.Grammar
} {
	return []struct {
		name    string
		grammar func() query.Grammar
	}{
		{"mysql", func() query.Grammar { return grammars.NewMySQLGrammar() }},
		{"mariadb", func() query.Grammar { return grammars.NewMariaDBGrammar() }},
		{"postgres", func() query.Grammar { return grammars.NewPostgresGrammar() }},
		{"sqlite", func() query.Grammar { return grammars.NewSQLiteGrammar() }},
	}
}

// compiles runs one compilation against every grammar and compares the result
// with the string expected for that dialect.
func compiles(t *testing.T, compile func(query.Grammar) string, want map[string]string) {
	t.Helper()

	for _, dialect := range dialects() {
		t.Run(dialect.name, func(t *testing.T) {
			expected, ok := want[dialect.name]
			if !ok {
				t.Fatalf("the test does not say what %s should compile to", dialect.name)
			}

			got := compile(dialect.grammar())
			if got != expected {
				t.Errorf("compiled\n  %s\nwanted\n  %s", got, expected)
			}
		})
	}
}

func TestCompileSelect(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").Where("votes", ">", 100).OrderBy("name").Limit(10).Offset(5)
		return q.ToSQL()
	}

	compiles(t, compile, map[string]string{
		"mysql":    "select * from `users` where `votes` > ? order by `name` asc limit 10 offset 5",
		"mariadb":  "select * from `users` where `votes` > ? order by `name` asc limit 10 offset 5",
		"postgres": `select * from "users" where "votes" > ? order by "name" asc limit 10 offset 5`,
		"sqlite":   `select * from "users" where "votes" > ? order by "name" asc limit 10 offset 5`,
	})
}

func TestCompileSelectWithJoinAndNestedWhere(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").
			Join("posts", "users.id", "=", "posts.user_id").
			Where(func(nested *query.Builder) {
				nested.Where("posts.published", true).OrWhere("posts.pinned", true)
			}).
			WhereIn("users.id", []any{1, 2, 3}).
			WhereNull("users.deleted_at")
		return q.ToSQL()
	}

	compiles(t, compile, map[string]string{
		"mysql": "select * from `users` inner join `posts` on `users`.`id` = `posts`.`user_id` " +
			"where (`posts`.`published` = ? or `posts`.`pinned` = ?) and `users`.`id` in (?, ?, ?) and `users`.`deleted_at` is null",
		"mariadb": "select * from `users` inner join `posts` on `users`.`id` = `posts`.`user_id` " +
			"where (`posts`.`published` = ? or `posts`.`pinned` = ?) and `users`.`id` in (?, ?, ?) and `users`.`deleted_at` is null",
		"postgres": `select * from "users" inner join "posts" on "users"."id" = "posts"."user_id" ` +
			`where ("posts"."published" = ? or "posts"."pinned" = ?) and "users"."id" in (?, ?, ?) and "users"."deleted_at" is null`,
		"sqlite": `select * from "users" inner join "posts" on "users"."id" = "posts"."user_id" ` +
			`where ("posts"."published" = ? or "posts"."pinned" = ?) and "users"."id" in (?, ?, ?) and "users"."deleted_at" is null`,
	})
}

// TestCompileSelectWithEmptyWhereIn is the clause nobody writes on purpose and
// everybody hits: a filter over a list that turned out empty. It has to match
// nothing. Dropping it would match everything, which on a tenant scoped query
// is the whole table.
func TestCompileSelectWithEmptyWhereIn(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").WhereIn("id", nil)
		return q.ToSQL()
	}

	compiles(t, compile, map[string]string{
		"mysql":    "select * from `users` where 0 = 1",
		"mariadb":  "select * from `users` where 0 = 1",
		"postgres": `select * from "users" where 0 = 1`,
		"sqlite":   `select * from "users" where 0 = 1`,
	})
}

func TestCompileSelectWithGroupByAndHaving(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("orders").Select("status").GroupBy("status").Having("total", ">", 100)
		return q.ToSQL()
	}

	compiles(t, compile, map[string]string{
		"mysql":    "select `status` from `orders` group by `status` having `total` > ?",
		"mariadb":  "select `status` from `orders` group by `status` having `total` > ?",
		"postgres": `select "status" from "orders" group by "status" having "total" > ?`,
		"sqlite":   `select "status" from "orders" group by "status" having "total" > ?`,
	})
}

func TestCompileAggregate(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").SetAggregate("count", []any{"*"})
		return q.ToSQL()
	}

	compiles(t, compile, map[string]string{
		"mysql":    "select count(*) as aggregate from `users`",
		"mariadb":  "select count(*) as aggregate from `users`",
		"postgres": `select count(*) as aggregate from "users"`,
		"sqlite":   `select count(*) as aggregate from "users"`,
	})
}

// TestCompileDistinct is the one place Postgres accepts columns where the
// others accept only the keyword.
func TestCompileDistinct(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").Select("name", "email").Distinct("name")
		return q.ToSQL()
	}

	compiles(t, compile, map[string]string{
		"mysql":    "select distinct `name`, `email` from `users`",
		"mariadb":  "select distinct `name`, `email` from `users`",
		"postgres": `select distinct on ("name") "name", "email" from "users"`,
		"sqlite":   `select distinct "name", "email" from "users"`,
	})
}

// TestCompileUnion is SQLite's parenthesis problem: it will not take a
// parenthesised select as a union operand, so the operand gets a select of its
// own.
func TestCompileUnion(t *testing.T) {
	compile := func(g query.Grammar) string {
		first := query.NewBuilder(nil, g, nil)
		first.From("users").Where("id", 1)

		second := query.NewBuilder(nil, g, nil)
		second.From("users").Where("id", 2)

		return first.Union(second).ToSQL()
	}

	compiles(t, compile, map[string]string{
		"mysql":    "(select * from `users` where `id` = ?) union (select * from `users` where `id` = ?)",
		"mariadb":  "(select * from `users` where `id` = ?) union (select * from `users` where `id` = ?)",
		"postgres": `(select * from "users" where "id" = ?) union (select * from "users" where "id" = ?)`,
		"sqlite": `select * from (select * from "users" where "id" = ?) ` +
			`union select * from (select * from "users" where "id" = ?)`,
	})
}

// TestCompileLock: SQLite locks the file rather than the row, so it has nothing
// to say; MySQL and Postgres disagree about the name of a shared lock.
func TestCompileLock(t *testing.T) {
	compileForUpdate := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").LockForUpdate()
		return q.ToSQL()
	}

	compiles(t, compileForUpdate, map[string]string{
		"mysql":    "select * from `users` for update",
		"mariadb":  "select * from `users` for update",
		"postgres": `select * from "users" for update`,
		"sqlite":   `select * from "users"`,
	})

	compileShared := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").SharedLock()
		return q.ToSQL()
	}

	compiles(t, compileShared, map[string]string{
		"mysql":    "select * from `users` lock in share mode",
		"mariadb":  "select * from `users` lock in share mode",
		"postgres": `select * from "users" for share`,
		"sqlite":   `select * from "users"`,
	})
}

func TestCompileRandom(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").InRandomOrder()
		return q.ToSQL()
	}

	compiles(t, compile, map[string]string{
		"mysql":    "select * from `users` order by RAND()",
		"mariadb":  "select * from `users` order by RAND()",
		"postgres": `select * from "users" order by RANDOM()`,
		"sqlite":   `select * from "users" order by RANDOM()`,
	})
}

// TestCompileRandomWithNonNumericSeed: the seed is written into the statement,
// so one that is not a number is dropped rather than interpolated.
func TestCompileRandomWithNonNumericSeed(t *testing.T) {
	compiles(t, func(g query.Grammar) string { return g.CompileRandom("1); drop table users; --") }, map[string]string{
		"mysql":    "RAND()",
		"mariadb":  "RAND()",
		"postgres": "RANDOM()",
		"sqlite":   "RANDOM()",
	})

	compiles(t, func(g query.Grammar) string { return g.CompileRandom("42") }, map[string]string{
		"mysql":    "RAND(42)",
		"mariadb":  "RAND(42)",
		"postgres": "RANDOM()",
		"sqlite":   "RANDOM()",
	})
}

func TestCompileInsert(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users")
		return g.CompileInsert(q, []map[string]any{{"email": "a@b.c", "votes": 1}})
	}

	compiles(t, compile, map[string]string{
		"mysql":    "insert into `users` (`email`, `votes`) values (?, ?)",
		"mariadb":  "insert into `users` (`email`, `votes`) values (?, ?)",
		"postgres": `insert into "users" ("email", "votes") values (?, ?)`,
		"sqlite":   `insert into "users" ("email", "votes") values (?, ?)`,
	})
}

// TestCompileInsertOfNothing is the row of defaults, which is the one insert
// the three engines spell three ways.
func TestCompileInsertOfNothing(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users")
		return g.CompileInsert(q, nil)
	}

	compiles(t, compile, map[string]string{
		"mysql":    "insert into `users` () values ()",
		"mariadb":  "insert into `users` () values ()",
		"postgres": `insert into "users" default values`,
		"sqlite":   `insert into "users" default values`,
	})
}

func TestCompileInsertOrIgnore(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users")
		return g.CompileInsertOrIgnore(q, []map[string]any{{"email": "a@b.c"}})
	}

	compiles(t, compile, map[string]string{
		"mysql":    "insert ignore into `users` (`email`) values (?)",
		"mariadb":  "insert ignore into `users` (`email`) values (?)",
		"postgres": `insert into "users" ("email") values (?) on conflict do nothing`,
		"sqlite":   `insert or ignore into "users" ("email") values (?)`,
	})
}

// TestCompileInsertGetID: only Postgres has to be told to hand the identifier
// back, which is why PostgresProcessor reads it from a result set and the
// others ask the connection.
func TestCompileInsertGetID(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users")
		return g.CompileInsertGetID(q, map[string]any{"email": "a@b.c"}, "")
	}

	compiles(t, compile, map[string]string{
		"mysql":    "insert into `users` (`email`) values (?)",
		"mariadb":  "insert into `users` (`email`) values (?)",
		"postgres": `insert into "users" ("email") values (?) returning "id"`,
		"sqlite":   `insert into "users" ("email") values (?)`,
	})
}

func TestCompileUpsert(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users")
		return g.CompileUpsert(q,
			[]map[string]any{{"email": "a@b.c", "votes": 1}},
			[]string{"email"},
			[]string{"votes"},
		)
	}

	compiles(t, compile, map[string]string{
		"mysql":    "insert into `users` (`email`, `votes`) values (?, ?) on duplicate key update `votes` = values(`votes`)",
		"mariadb":  "insert into `users` (`email`, `votes`) values (?, ?) on duplicate key update `votes` = values(`votes`)",
		"postgres": `insert into "users" ("email", "votes") values (?, ?) on conflict ("email") do update set "votes" = "excluded"."votes"`,
		"sqlite":   `insert into "users" ("email", "votes") values (?, ?) on conflict ("email") do update set "votes" = "excluded"."votes"`,
	})
}

func TestCompileUpdate(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").Where("id", 1)
		return g.CompileUpdate(q, map[string]any{"email": "a@b.c", "votes": 2})
	}

	compiles(t, compile, map[string]string{
		"mysql":    "update `users` set `email` = ?, `votes` = ? where `id` = ?",
		"mariadb":  "update `users` set `email` = ?, `votes` = ? where `id` = ?",
		"postgres": `update "users" set "email" = ?, "votes" = ? where "id" = ?`,
		"sqlite":   `update "users" set "email" = ?, "votes" = ? where "id" = ?`,
	})
}

// TestCompileUpdateWithLimit is the sharpest difference in the package: MySQL
// takes a limit on an update, and the other two have to name the rows by their
// physical address and choose them with a select.
func TestCompileUpdateWithLimit(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").Where("id", 1).Limit(5)
		return g.CompileUpdate(q, map[string]any{"email": "a@b.c"})
	}

	compiles(t, compile, map[string]string{
		"mysql":    "update `users` set `email` = ? where `id` = ? limit 5",
		"mariadb":  "update `users` set `email` = ? where `id` = ? limit 5",
		"postgres": `update "users" set "email" = ? where "ctid" in (select "users"."ctid" from "users" where "id" = ? limit 5)`,
		"sqlite":   `update "users" set "email" = ? where "rowid" in (select "users"."rowid" from "users" where "id" = ? limit 5)`,
	})
}

func TestCompileDelete(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").Where("id", 1)
		return g.CompileDelete(q)
	}

	compiles(t, compile, map[string]string{
		"mysql":    "delete from `users` where `id` = ?",
		"mariadb":  "delete from `users` where `id` = ?",
		"postgres": `delete from "users" where "id" = ?`,
		"sqlite":   `delete from "users" where "id" = ?`,
	})
}

func TestCompileDeleteWithLimit(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").Where("id", 1).Limit(5)
		return g.CompileDelete(q)
	}

	compiles(t, compile, map[string]string{
		"mysql":    "delete from `users` where `id` = ? limit 5",
		"mariadb":  "delete from `users` where `id` = ? limit 5",
		"postgres": `delete from "users" where "ctid" in (select "users"."ctid" from "users" where "id" = ? limit 5)`,
		"sqlite":   `delete from "users" where "rowid" in (select "users"."rowid" from "users" where "id" = ? limit 5)`,
	})
}

// TestCompileTruncate: SQLite has no truncate, so it is two deletes -- one of
// them bound, which is why the return is a statement to bindings map.
func TestCompileTruncate(t *testing.T) {
	want := map[string]map[string][]any{
		"mysql":    {"truncate table `users`": {}},
		"mariadb":  {"truncate table `users`": {}},
		"postgres": {`truncate "users" restart identity cascade`: {}},
		"sqlite": {
			"delete from sqlite_sequence where name = ?": {"users"},
			`delete from "users"`:                        {},
		},
	}

	for _, dialect := range dialects() {
		t.Run(dialect.name, func(t *testing.T) {
			g := dialect.grammar()
			q := query.NewBuilder(nil, g, nil)
			q.From("users")

			got := g.CompileTruncate(q)
			expected := want[dialect.name]

			if len(got) != len(expected) {
				t.Fatalf("compiled %d statements, wanted %d: %v", len(got), len(expected), got)
			}

			for statement, bindings := range expected {
				gotBindings, ok := got[statement]
				if !ok {
					t.Fatalf("did not compile %q; got %v", statement, got)
				}
				if len(gotBindings) != len(bindings) {
					t.Fatalf("%q got bindings %v, wanted %v", statement, gotBindings, bindings)
				}
				for i := range bindings {
					if gotBindings[i] != bindings[i] {
						t.Fatalf("%q binding %d is %v, wanted %v", statement, i, gotBindings[i], bindings[i])
					}
				}
			}
		})
	}
}

// TestWrapJSONSelector is where the three engines agree least: the same column
// and path, three syntaxes.
func TestWrapJSONSelector(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").Where("options->enabled", true)
		return q.ToSQL()
	}

	compiles(t, compile, map[string]string{
		"mysql":    "select * from `users` where json_unquote(json_extract(`options`, '$.\"enabled\"')) = ?",
		"mariadb":  "select * from `users` where json_value(`options`, '$.\"enabled\"') = ?",
		"postgres": `select * from "users" where "options"->>'enabled' = ?`,
		"sqlite":   `select * from "users" where json_extract("options", '$."enabled"') = ?`,
	})
}

func TestCompileJSONLength(t *testing.T) {
	compile := func(g query.Grammar) string {
		sql, err := g.(interface {
			CompileJSONLength(column any, operator, value string) (string, error)
		}).CompileJSONLength("options->languages", ">", "?")
		if err != nil {
			return "error: " + err.Error()
		}
		return sql
	}

	compiles(t, compile, map[string]string{
		"mysql":    "json_length(`options`, '$.\"languages\"') > ?",
		"mariadb":  "json_length(`options`, '$.\"languages\"') > ?",
		"postgres": `jsonb_array_length(("options"->'languages')::jsonb) > ?`,
		"sqlite":   `json_array_length("options", '$."languages"') > ?`,
	})
}

func TestCompileJSONContains(t *testing.T) {
	compile := func(g query.Grammar) string {
		sql, err := g.(interface {
			CompileJSONContains(column any, value string) (string, error)
		}).CompileJSONContains("options->languages", "?")
		if err != nil {
			return "error: " + err.Error()
		}
		return sql
	}

	compiles(t, compile, map[string]string{
		"mysql":    "json_contains(`options`, ?, '$.\"languages\"')",
		"mariadb":  "json_contains(`options`, ?, '$.\"languages\"')",
		"postgres": `("options"->'languages')::jsonb @> ?`,
		"sqlite":   `exists (select 1 from json_each("options", '$."languages"') where "json_each"."value" is ?)`,
	})
}

// TestTablePrefix: the prefix is applied by WrapTable and by nothing else,
// which is why a qualified column picks it up through its table half.
func TestTablePrefix(t *testing.T) {
	compile := func(g query.Grammar) string {
		g.SetTablePrefix("p_")
		q := query.NewBuilder(nil, g, nil)
		q.From("users").Where("users.id", 1)
		return q.ToSQL()
	}

	compiles(t, compile, map[string]string{
		"mysql":    "select * from `p_users` where `p_users`.`id` = ?",
		"mariadb":  "select * from `p_users` where `p_users`.`id` = ?",
		"postgres": `select * from "p_users" where "p_users"."id" = ?`,
		"sqlite":   `select * from "p_users" where "p_users"."id" = ?`,
	})
}

// TestSetTablePrefixReturnsTheGrammar guards the one method query.BaseGrammar
// cannot answer on its own: it returns nil, because it cannot name the grammar
// that embedded it.
func TestSetTablePrefixReturnsTheGrammar(t *testing.T) {
	for _, dialect := range dialects() {
		t.Run(dialect.name, func(t *testing.T) {
			g := dialect.grammar()
			if g.SetTablePrefix("p_") == nil {
				t.Fatal("SetTablePrefix returned nil instead of the grammar")
			}
			if g.GetTablePrefix() != "p_" {
				t.Fatalf("the prefix is %q, wanted %q", g.GetTablePrefix(), "p_")
			}
		})
	}
}

// TestExpressionIsNotWrapped: an Expression is how a caller says "this is SQL,
// not a name", and every grammar has to leave it alone.
func TestExpressionIsNotWrapped(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").Select(query.Raw("count(*) as total")).Where("id", query.Raw("2 + 3"))
		return q.ToSQL()
	}

	compiles(t, compile, map[string]string{
		"mysql":    "select count(*) as total from `users` where `id` = 2 + 3",
		"mariadb":  "select count(*) as total from `users` where `id` = 2 + 3",
		"postgres": `select count(*) as total from "users" where "id" = 2 + 3`,
		"sqlite":   `select count(*) as total from "users" where "id" = 2 + 3`,
	})
}

// TestAliasIsWrappedInPieces: "users as u" is two identifiers and a keyword,
// never one identifier with spaces in it.
func TestAliasIsWrappedInPieces(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users", "u").Select("u.id as identifier")
		return q.ToSQL()
	}

	compiles(t, compile, map[string]string{
		"mysql":    "select `u`.`id` as `identifier` from `users` as `u`",
		"mariadb":  "select `u`.`id` as `identifier` from `users` as `u`",
		"postgres": `select "u"."id" as "identifier" from "users" as "u"`,
		"sqlite":   `select "u"."id" as "identifier" from "users" as "u"`,
	})
}

// TestQuoteInsideAnIdentifierIsDoubled: dropping it would silently rename the
// column, and leaving it would end the identifier early.
func TestQuoteInsideAnIdentifierIsDoubled(t *testing.T) {
	compile := func(g query.Grammar) string {
		q := query.NewBuilder(nil, g, nil)
		q.From("users").Select("we`ird\"one")
		return q.ToSQL()
	}

	compiles(t, compile, map[string]string{
		"mysql":    "select `we``ird\"one` from `users`",
		"mariadb":  "select `we``ird\"one` from `users`",
		"postgres": `select "we` + "`" + `ird""one" from "users"`,
		"sqlite":   `select "we` + "`" + `ird""one" from "users"`,
	})
}

// TestUnsupportedClauseIsFalseRatherThanDropped: a filter the grammar cannot
// spell must not disappear. SQLite has no fulltext search, and the query has to
// come back empty rather than unfiltered.
func TestUnsupportedClauseIsFalseRatherThanDropped(t *testing.T) {
	g := grammars.NewSQLiteGrammar()

	q := query.NewBuilder(nil, g, nil)
	q.From("users")
	q.Wheres = append(q.Wheres, query.Where{
		Type:    "Fulltext",
		Columns: []any{"body"},
		Value:   "arandu",
		Boolean: "and",
	})

	sql := g.CompileSelect(q)

	if !contains(sql, "1 = 0") {
		t.Fatalf("the clause was not compiled false: %s", sql)
	}
	if !contains(sql, "fulltext") {
		t.Fatalf("the clause does not carry its reason: %s", sql)
	}
}

// TestUnknownWhereTypeIsFalseRatherThanDropped is the same guarantee for a
// clause type no compiler claims, which is what a builder written against a
// newer grammar would produce.
func TestUnknownWhereTypeIsFalseRatherThanDropped(t *testing.T) {
	for _, dialect := range dialects() {
		t.Run(dialect.name, func(t *testing.T) {
			g := dialect.grammar()

			q := query.NewBuilder(nil, g, nil)
			q.From("users")
			q.Wheres = append(q.Wheres, query.Where{Type: "SomethingNobodyWrote", Boolean: "and"})

			if sql := g.CompileSelect(q); !contains(sql, "1 = 0") {
				t.Fatalf("the clause was not compiled false: %s", sql)
			}
		})
	}
}

// TestPrepareBindingsForUpdate is the other half of TestCompileUpdate, and the
// half that fails silently when it is wrong: the values have to arrive in the
// order the placeholders were written, or every binding after the first mistake
// lands in the wrong column.
//
// MySQL compiles its joins before the set list and Postgres and SQLite compile
// theirs after, which is the whole reason the two orders differ.
func TestPrepareBindingsForUpdate(t *testing.T) {
	bindings := func() map[string][]any {
		return map[string][]any{
			"select": {"from-select"},
			"join":   {"from-join"},
			"where":  {"from-where"},
		}
	}

	compile := func(g query.Grammar) string {
		return fmt.Sprint(g.PrepareBindingsForUpdate(bindings(), map[string]any{"a": 1, "b": 2}))
	}

	compiles(t, compile, map[string]string{
		"mysql":    "[from-join 1 2 from-where]",
		"mariadb":  "[from-join 1 2 from-where]",
		"postgres": "[1 2 from-join from-where]",
		"sqlite":   "[1 2 from-join from-where]",
	})
}

// TestPrepareBindingsForUpdateKeepsExpressions: an expression was written into
// the statement instead of a placeholder, so it must not be bound -- and it is
// Builder.CleanBindings that drops it, by recognising the type. Unwrapping it
// here would hide it from that check and shift every binding after it by one.
func TestPrepareBindingsForUpdateKeepsExpressions(t *testing.T) {
	for _, dialect := range dialects() {
		t.Run(dialect.name, func(t *testing.T) {
			got := dialect.grammar().PrepareBindingsForUpdate(
				map[string][]any{},
				map[string]any{"votes": query.Raw("votes + 1")},
			)

			if len(got) != 1 {
				t.Fatalf("got %d bindings, wanted 1: %v", len(got), got)
			}
			if !query.IsExpression(got[0]) {
				t.Fatalf("the expression came back as %#v, no longer recognisable as one", got[0])
			}
		})
	}
}

// TestPostgresOperatorsAreOffered guards the list Builder checks an operator
// against: Postgres accepts several the others do not, and rejecting them would
// make its own JSON operators unusable.
func TestPostgresOperatorsAreOffered(t *testing.T) {
	operators := grammars.NewPostgresGrammar().GetOperators()

	for _, operator := range []string{"@>", "?|", "ilike", "is distinct from"} {
		if !slices.Contains(operators, operator) {
			t.Errorf("the Postgres grammar does not offer %q", operator)
		}
	}
}

func TestOperatorsAreValidatedAgainstTheEffectiveDialect(t *testing.T) {
	tests := []struct {
		name     string
		grammar  query.Grammar
		operator string
		valid    bool
	}{
		{name: "mysql extension", grammar: grammars.NewMySQLGrammar(), operator: "SoUnDs LiKe", valid: true},
		{name: "postgres extension", grammar: grammars.NewPostgresGrammar(), operator: "@>", valid: true},
		{name: "postgres operator on mysql", grammar: grammars.NewMySQLGrammar(), operator: "@>", valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			builder := query.NewBuilder(nil, test.grammar, nil).From("documents").Where("body", test.operator, "value")
			sql := builder.ToSQL()
			if test.valid && sql == "" {
				t.Fatalf("supported operator %q was refused: %v", test.operator, builder.Err())
			}
			if !test.valid && builder.Err() == nil {
				t.Fatalf("unsupported operator %q compiled as %s", test.operator, sql)
			}
		})
	}
}

type extendedOperatorGrammar struct{ query.Grammar }

func (g *extendedOperatorGrammar) GetOperators() []string {
	return append(g.Grammar.GetOperators(), "matches phrase")
}

func TestPromotedCompilerUsesTheExternalGrammarOperatorPolicy(t *testing.T) {
	g := &extendedOperatorGrammar{Grammar: grammars.NewMySQLGrammar()}
	b := query.NewBuilder(nil, g, nil).
		From("documents").
		Where("body", "matches phrase", "value")

	if got := b.ToSQL(); got != "select * from `documents` where `body` matches phrase ?" {
		t.Fatalf("ToSQL() = %q, Err() = %v", got, b.Err())
	}
}

func TestAnInvalidCommonOperatorIsNeitherStoredNorDirectlyCompiled(t *testing.T) {
	g := grammars.NewMySQLGrammar()
	b := query.NewBuilder(nil, g, nil).
		From("users").
		Where("id", "= 0 OR 1 =", 1)

	if len(b.Wheres) != 0 {
		t.Errorf("the invalid operator was stored in %v", b.Wheres)
	}
	if sql := g.CompileSelect(b); sql != "" {
		t.Errorf("the public compiler emitted hostile SQL: %s", sql)
	}
	if !errors.Is(b.Err(), query.ErrInvalidOperator) {
		t.Fatalf("Err() = %v, want ErrInvalidOperator", b.Err())
	}
}

func TestPublicStatementCompilersFailClosedAfterDirectOperatorMutation(t *testing.T) {
	actions := map[string]func(query.Grammar, *query.Builder) bool{
		"select": func(g query.Grammar, b *query.Builder) bool {
			return g.CompileSelect(b) != ""
		},
		"exists": func(g query.Grammar, b *query.Builder) bool {
			return g.CompileExists(b) != ""
		},
		"insert": func(g query.Grammar, b *query.Builder) bool {
			return g.CompileInsert(b, []map[string]any{{"name": "Ada"}}) != ""
		},
		"insert or ignore": func(g query.Grammar, b *query.Builder) bool {
			return g.CompileInsertOrIgnore(b, []map[string]any{{"name": "Ada"}}) != ""
		},
		"insert get id": func(g query.Grammar, b *query.Builder) bool {
			return g.CompileInsertGetID(b, map[string]any{"name": "Ada"}, "id") != ""
		},
		"update": func(g query.Grammar, b *query.Builder) bool {
			return g.CompileUpdate(b, map[string]any{"name": "Ada"}) != ""
		},
		"upsert": func(g query.Grammar, b *query.Builder) bool {
			return g.CompileUpsert(b, []map[string]any{{"name": "Ada"}}, []string{"name"}, []string{"name"}) != ""
		},
		"delete": func(g query.Grammar, b *query.Builder) bool {
			return g.CompileDelete(b) != ""
		},
		"truncate": func(g query.Grammar, b *query.Builder) bool {
			return len(g.CompileTruncate(b)) != 0
		},
	}

	for _, dialect := range dialects() {
		for name, compiled := range actions {
			t.Run(dialect.name+"/"+name, func(t *testing.T) {
				g := dialect.grammar()
				b := query.NewBuilder(nil, g, nil).From("users")
				b.Wheres = append(b.Wheres, query.Where{
					Type: "Basic", Column: "id", Operator: "= 0 OR 1 =", Value: 1, Boolean: "and",
				})

				if compiled(g, b) {
					t.Fatal("the public compiler produced a statement with an invalid operator")
				}
				if !errors.Is(b.Err(), query.ErrInvalidOperator) {
					t.Fatalf("Err() = %v, want ErrInvalidOperator", b.Err())
				}
			})
		}
	}
}

func TestPublicGroupLimitCompilersFailClosedAfterDirectOperatorMutation(t *testing.T) {
	type groupLimitCompiler interface {
		CompileGroupLimit(*query.Builder) string
	}

	for _, dialect := range dialects() {
		t.Run(dialect.name, func(t *testing.T) {
			g := dialect.grammar()
			compiler, ok := g.(groupLimitCompiler)
			if !ok {
				t.Fatalf("%T does not expose CompileGroupLimit", g)
			}
			b := query.NewBuilder(nil, g, nil).From("users").GroupLimit(1, "team_id")
			b.Wheres = append(b.Wheres, query.Where{
				Type: "Basic", Column: "id", Operator: "not an operator", Value: 1, Boolean: "and",
			})

			if sql := compiler.CompileGroupLimit(b); sql != "" {
				t.Fatalf("CompileGroupLimit() emitted an unfiltered statement: %s", sql)
			}
			if !errors.Is(b.Err(), query.ErrInvalidOperator) {
				t.Fatalf("Err() = %v, want ErrInvalidOperator", b.Err())
			}
		})
	}
}

func TestPublicUpdateAndDeleteHelpersFailClosedAfterDirectOperatorMutation(t *testing.T) {
	type statementHelpers interface {
		CompileUpdateWithoutJoins(*query.Builder, string, string, string) string
		CompileUpdateWithJoins(*query.Builder, string, string, string) string
		CompileDeleteWithoutJoins(*query.Builder, string, string) string
		CompileDeleteWithJoins(*query.Builder, string, string) string
	}

	actions := map[string]func(statementHelpers, *query.Builder) string{
		"update without joins": func(g statementHelpers, b *query.Builder) string {
			return g.CompileUpdateWithoutJoins(b, "users", "name = ?", "")
		},
		"update with joins": func(g statementHelpers, b *query.Builder) string {
			return g.CompileUpdateWithJoins(b, "users", "name = ?", "")
		},
		"delete without joins": func(g statementHelpers, b *query.Builder) string {
			return g.CompileDeleteWithoutJoins(b, "users", "")
		},
		"delete with joins": func(g statementHelpers, b *query.Builder) string {
			return g.CompileDeleteWithJoins(b, "users", "")
		},
	}

	for _, dialect := range dialects() {
		for name, compile := range actions {
			t.Run(dialect.name+"/"+name, func(t *testing.T) {
				g := dialect.grammar()
				helpers, ok := g.(statementHelpers)
				if !ok {
					t.Fatalf("%T does not expose update/delete statement helpers", g)
				}
				b := query.NewBuilder(nil, g, nil).From("users")
				b.Wheres = append(b.Wheres, query.Where{
					Type: "Basic", Column: "tenant_id", Operator: "not an operator", Value: "acme", Boolean: "and",
				})

				if sql := compile(helpers, b); sql != "" {
					t.Fatalf("compiler emitted an unfiltered statement: %s", sql)
				}
				if !errors.Is(b.Err(), query.ErrInvalidOperator) {
					t.Fatalf("Err() = %v, want ErrInvalidOperator", b.Err())
				}
			})
		}
	}
}

func TestPostgresCustomOperatorsOnlyRegisterSafeSymbolTokens(t *testing.T) {
	grammars.CustomOperators([]string{"@#", "safe_word", "= OR", "=;", "/*", "--"})
	operators := grammars.NewPostgresGrammar().GetOperators()

	if !slices.Contains(operators, "@#") {
		t.Fatal("the safe custom Postgres operator was not registered")
	}
	for _, unsafe := range []string{"safe_word", "= OR", "=;", "/*", "--"} {
		if slices.Contains(operators, unsafe) {
			t.Errorf("unsafe custom Postgres operator %q was registered", unsafe)
		}
	}

	b := query.NewBuilder(nil, grammars.NewPostgresGrammar(), nil).
		From("documents").
		Where("metadata", "@#", "value")
	if sql := b.ToSQL(); sql == "" {
		t.Fatalf("registered custom operator was refused: %v", b.Err())
	}
}

func TestPostgresCustomOperatorRegisteredAfterBuilderCreationIsLive(t *testing.T) {
	b := query.NewBuilder(nil, grammars.NewPostgresGrammar(), nil).From("documents")
	grammars.CustomOperators([]string{"#@#"})
	b.Where("metadata", "#@#", "value")

	if sql := b.ToSQL(); sql == "" {
		t.Fatalf("late registered custom operator was refused: %v", b.Err())
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
