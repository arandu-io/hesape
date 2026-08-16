package grammars_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/schema"
)

// grant is the Grant every test compiles under. A migration runner has no
// request to authorize from, so it takes the named escape hatch, which refuses
// to issue a Grant without a tenant.
func grant() auth.Grant { return auth.SystemGrant(schema.ActionMigrate, "acme") }

func compile(t *testing.T, conn *fakeConnection, table string, build func(*schema.Blueprint)) []string {
	t.Helper()
	blueprint := schema.NewBlueprint(conn, table, build)
	sql, err := blueprint.ToSQL(context.Background(), grant())
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	return sql
}

func assertSQL(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d statements, want %d\ngot:  %#v\nwant: %#v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d\ngot:  %s\nwant: %s", i, got[i], want[i])
		}
	}
}

// TestCreateTableAcrossDrivers is the point of having three grammars: the same
// blueprint has to come out as the DDL each engine actually accepts, and the
// column types are where they disagree most.
func TestCreateTableAcrossDrivers(t *testing.T) {
	build := func(table *schema.Blueprint) {
		table.Create()
		table.ID("")
		table.String("email", 255).Unique()
		table.Text("bio").Nullable()
		table.JSON("meta")
		table.JSONB("payload")
		table.Boolean("active").Default(true)
		table.Binary("avatar", 0, false)
		table.UUID("public_id")
		table.Timestamps()
	}

	t.Run("mysql", func(t *testing.T) {
		assertSQL(t, compile(t, newFake("mysql"), "users", build),
			"create table `users` ("+
				"`id` bigint unsigned not null auto_increment primary key, "+
				"`email` varchar(255) not null, "+
				"`bio` text null, "+
				"`meta` json not null, "+
				"`payload` json not null, "+
				"`active` tinyint(1) not null default '1', "+
				"`avatar` blob not null, "+
				"`public_id` char(36) not null, "+
				"`created_at` timestamp null, "+
				"`updated_at` timestamp null)",
			"alter table `users` add unique `users_email_unique`(`email`)")
	})

	t.Run("pgsql", func(t *testing.T) {
		assertSQL(t, compile(t, newFake("pgsql"), "users", build),
			`create table "users" (`+
				`"id" bigserial not null primary key, `+
				`"email" varchar(255) not null, `+
				`"bio" text null, `+
				`"meta" json not null, `+
				`"payload" jsonb not null, `+
				`"active" boolean not null default '1', `+
				`"avatar" bytea not null, `+
				`"public_id" uuid not null, `+
				`"created_at" timestamp(0) without time zone null, `+
				`"updated_at" timestamp(0) without time zone null)`,
			`alter table "users" add constraint "users_email_unique" unique ("email")`)
	})

	t.Run("sqlite", func(t *testing.T) {
		assertSQL(t, compile(t, newFake("sqlite"), "users", build),
			`create table "users" (`+
				`"id" integer primary key autoincrement not null, `+
				`"email" varchar not null, `+
				`"bio" text, `+
				`"meta" text not null, `+
				`"payload" text not null, `+
				`"active" tinyint(1) not null default '1', `+
				`"avatar" blob not null, `+
				`"public_id" varchar not null, `+
				`"created_at" datetime, `+
				`"updated_at" datetime)`,
			`create unique index "users_email_unique" on "users" ("email")`)
	})
}

// TestNativeJSONOnSQLite covers the two connection options that decide whether
// SQLite stores JSON in its own type or in text, because the answer is
// configuration rather than a version.
func TestNativeJSONOnSQLite(t *testing.T) {
	conn := newFake("sqlite")
	conn.config["use_native_json"] = "true"
	conn.config["use_native_jsonb"] = "true"

	assertSQL(t, compile(t, conn, "documents", func(table *schema.Blueprint) {
		table.Create()
		table.JSON("body")
		table.JSONB("index")
	}),
		`create table "documents" ("body" json not null, "index" jsonb not null)`)
}

// TestTimestampsFamily covers what a migration types most often.
func TestTimestampsFamily(t *testing.T) {
	build := func(table *schema.Blueprint) {
		table.Create()
		table.TimestampsTz()
		table.SoftDeletes("")
		table.Timestamp("published_at").UseCurrent().UseCurrentOnUpdate()
	}

	t.Run("mysql", func(t *testing.T) {
		assertSQL(t, compile(t, newFake("mysql"), "posts", build),
			"create table `posts` ("+
				"`created_at` timestamp null, "+
				"`updated_at` timestamp null, "+
				"`deleted_at` timestamp null, "+
				"`published_at` timestamp not null default CURRENT_TIMESTAMP on update CURRENT_TIMESTAMP)")
	})

	t.Run("pgsql", func(t *testing.T) {
		assertSQL(t, compile(t, newFake("pgsql"), "posts", build),
			`create table "posts" (`+
				`"created_at" timestamp(0) with time zone null, `+
				`"updated_at" timestamp(0) with time zone null, `+
				`"deleted_at" timestamp(0) without time zone null, `+
				`"published_at" timestamp(0) without time zone not null default CURRENT_TIMESTAMP)`)
	})

	t.Run("sqlite", func(t *testing.T) {
		assertSQL(t, compile(t, newFake("sqlite"), "posts", build),
			`create table "posts" (`+
				`"created_at" datetime, `+
				`"updated_at" datetime, `+
				`"deleted_at" datetime, `+
				`"published_at" datetime not null default CURRENT_TIMESTAMP)`)
	})
}

// TestForeignIDConstrained covers the other line every migration has: the table
// name is guessed from the column, and the key lands in the create statement on
// SQLite and in an alter on the other two.
func TestForeignIDConstrained(t *testing.T) {
	build := func(table *schema.Blueprint) {
		table.Create()
		table.ForeignID("company_id").Constrained().CascadeOnDelete().RestrictOnUpdate()
	}

	t.Run("mysql", func(t *testing.T) {
		assertSQL(t, compile(t, newFake("mysql"), "users", build),
			"create table `users` (`company_id` bigint unsigned not null)",
			"alter table `users` add constraint `users_company_id_foreign` "+
				"foreign key (`company_id`) references `companies` (`id`) "+
				"on delete cascade on update restrict")
	})

	t.Run("pgsql", func(t *testing.T) {
		assertSQL(t, compile(t, newFake("pgsql"), "users", build),
			`create table "users" ("company_id" bigint not null)`,
			`alter table "users" add constraint "users_company_id_foreign" `+
				`foreign key ("company_id") references "companies" ("id") `+
				`on delete cascade on update restrict`)
	})

	t.Run("sqlite", func(t *testing.T) {
		assertSQL(t, compile(t, newFake("sqlite"), "users", build),
			`create table "users" ("company_id" integer not null, `+
				`foreign key("company_id") references "companies"("id") `+
				`on delete cascade on update restrict)`)
	})
}

// TestConstrainedNamesTheTable checks the four shapes the guessed table name
// takes, because Constrained is the one place this component has to pluralise.
func TestConstrainedNamesTheTable(t *testing.T) {
	cases := map[string]string{
		"user_id":     "users",
		"company_id":  "companies",
		"address_id":  "addresses",
		"category_id": "categories",
	}

	for column, want := range cases {
		t.Run(column, func(t *testing.T) {
			sql := compile(t, newFake("mysql"), "things", func(table *schema.Blueprint) {
				table.Create()
				table.ForeignID(column).Constrained()
			})
			if !strings.Contains(sql[1], "references `"+want+"`") {
				t.Fatalf("got %s, want a reference to %s", sql[1], want)
			}
		})
	}
}

// TestMorphs covers the pair of columns plus the index, and the fact that the
// key column's type follows the package default.
func TestMorphs(t *testing.T) {
	assertSQL(t, compile(t, newFake("mysql"), "tags", func(table *schema.Blueprint) {
		table.Create()
		table.NullableMorphs("taggable")
	}),
		"create table `tags` (`taggable_type` varchar(255) null, `taggable_id` bigint unsigned null)",
		"alter table `tags` add index `tags_taggable_type_taggable_id_index`(`taggable_type`, `taggable_id`)")
}

// TestIndexesAndDrops walks the index family on an existing table.
func TestIndexesAndDrops(t *testing.T) {
	build := func(table *schema.Blueprint) {
		table.Index("email")
		table.Unique([]string{"team_id", "email"})
		table.Primary("id")
		table.DropIndex([]string{"name"})
		table.DropUnique("users_email_unique")
		table.RenameColumn("nick", "handle")
	}

	t.Run("mysql", func(t *testing.T) {
		assertSQL(t, compile(t, newFake("mysql"), "users", build),
			"alter table `users` add index `users_email_index`(`email`)",
			"alter table `users` add unique `users_team_id_email_unique`(`team_id`, `email`)",
			"alter table `users` add primary key (`id`)",
			"alter table `users` drop index `users_name_index`",
			"alter table `users` drop index `users_email_unique`",
			"alter table `users` rename column `nick` to `handle`")
	})

	t.Run("pgsql", func(t *testing.T) {
		assertSQL(t, compile(t, newFake("pgsql"), "users", build),
			`create index "users_email_index" on "users" ("email")`,
			`alter table "users" add constraint "users_team_id_email_unique" unique ("team_id", "email")`,
			`alter table "users" add primary key ("id")`,
			`drop index "users_name_index"`,
			`alter table "users" drop constraint "users_email_unique"`,
			`alter table "users" rename column "nick" to "handle"`)
	})
}

// TestPostgresOnlyIndexModifiers covers the modifiers only Postgres has, which
// is where IndexDefinition earns its existence.
func TestPostgresOnlyIndexModifiers(t *testing.T) {
	assertSQL(t, compile(t, newFake("pgsql"), "users", func(table *schema.Blueprint) {
		table.Unique("email").NullsNotDistinct().Deferrable().InitiallyImmediate(false)
		table.Index("body").Online().Algorithm("gin")
		table.FullText("body").Language("portuguese")
	}),
		`alter table "users" add constraint "users_email_unique" unique nulls not distinct ("email") deferrable initially deferred`,
		`create index concurrently "users_body_index" on "users" using gin ("body")`,
		`create index "users_body_fulltext" on "users" using gin ((to_tsvector('portuguese', "body")))`)
}

// TestChangeColumn covers the shape each driver gives an altered column, which
// is three different statements for one blueprint line.
func TestChangeColumn(t *testing.T) {
	build := func(table *schema.Blueprint) {
		table.String("email", 100).Nullable().Change()
	}

	t.Run("mysql", func(t *testing.T) {
		assertSQL(t, compile(t, newFake("mysql"), "users", build),
			"alter table `users` modify `email` varchar(100) null")
	})

	t.Run("pgsql", func(t *testing.T) {
		assertSQL(t, compile(t, newFake("pgsql"), "users", build),
			`alter table "users" alter column "email" type varchar(100), `+
				`alter column "email" drop not null, `+
				`alter column "email" drop default, `+
				`alter column "email" drop identity if exists`,
			`comment on column "users"."email" is NULL`)
	})
}

// TestChangeGeneratedColumn covers the three states a changed column's
// generation expression can be in, which a single nil field cannot tell
// apart on its own: never mentioning it leaves it alone, setting it to
// nothing drops it, and setting a new expression is refused, because
// Postgres cannot rewrite a generated column.
func TestChangeGeneratedColumn(t *testing.T) {
	dropped := compile(t, newFake("pgsql"), "users", func(table *schema.Blueprint) {
		table.String("slug", 100).VirtualAs(nil).Change()
	})
	if !strings.Contains(dropped[0], "drop expression if exists") {
		t.Fatalf("clearing the expression did not drop it: %s", dropped[0])
	}

	blueprint := schema.NewBlueprint(newFake("pgsql"), "users", func(table *schema.Blueprint) {
		table.String("slug", 100).VirtualAs("lower(name)").Change()
	})
	if _, err := blueprint.ToSQL(context.Background(), grant()); !errors.Is(err, schema.ErrUnsupported) {
		t.Fatalf("rewriting a generated column was accepted: %v", err)
	}
}

// TestSQLiteRebuild covers the table rebuild, which is the whole reason
// BlueprintState exists.
func TestSQLiteRebuild(t *testing.T) {
	conn := newFake("sqlite")
	conn.fkEnabled = true
	conn.columns = []schema.ColumnInfo{
		{Name: "id", TypeName: "integer", Type: "integer", AutoIncrement: true},
		{Name: "email", TypeName: "varchar", Type: "varchar", Nullable: true},
	}
	conn.indexes = []schema.IndexInfo{
		{Name: "primary", Columns: []string{"id"}, Primary: true, Unique: true},
		{Name: "users_email_unique", Columns: []string{"email"}, Unique: true},
	}

	assertSQL(t, compile(t, conn, "users", func(table *schema.Blueprint) {
		table.DropPrimary("id")
	}),
		"pragma foreign_keys = 0",
		`create table "__temp__users" ("id" integer primary key autoincrement not null, "email" varchar)`,
		`insert into "__temp__users" ("id", "email") select "id", "email" from "users"`,
		`drop table "users"`,
		`alter table "__temp__users" rename to "users"`,
		`create unique index "users_email_unique" on "users" ("email")`,
		"pragma foreign_keys = 1")
}

// TestTablePrefix checks that the prefix lands on the table and nowhere else,
// and that a schema-qualified name only prefixes its last segment.
func TestTablePrefix(t *testing.T) {
	conn := newFake("pgsql")
	conn.prefix = "app_"

	assertSQL(t, compile(t, conn, "reporting.users", func(table *schema.Blueprint) {
		table.Create()
		table.String("email", 255)
	}),
		`create table "reporting"."app_users" ("email" varchar(255) not null)`)
}

// TestDropAndRename covers the statements a down migration is made of.
func TestDropAndRename(t *testing.T) {
	for _, c := range []struct {
		driver string
		want   []string
	}{
		{"mysql", []string{
			"drop table if exists `users`",
			"rename table `users` to `people`",
			"alter table `users` drop `nick`, drop `bio`",
		}},
		{"pgsql", []string{
			`drop table if exists "users"`,
			`alter table "users" rename to "people"`,
			`alter table "users" drop column "nick", drop column "bio"`,
		}},
		{"sqlite", []string{
			`drop table if exists "users"`,
			`alter table "users" rename to "people"`,
			`alter table "users" drop column "nick"`,
			`alter table "users" drop column "bio"`,
		}},
	} {
		t.Run(c.driver, func(t *testing.T) {
			assertSQL(t, compile(t, newFake(c.driver), "users", func(table *schema.Blueprint) {
				table.DropIfExists()
				table.Rename("people")
				table.DropColumn("nick", "bio")
			}), c.want...)
		})
	}
}

// TestUnsupportedIsReportedNotSwallowed is the reason every compiler returns an
// error: a driver that quietly skips a command it cannot express leaves a
// migration half applied with nothing said.
func TestUnsupportedIsReportedNotSwallowed(t *testing.T) {
	blueprint := schema.NewBlueprint(newFake("sqlite"), "places", func(table *schema.Blueprint) {
		table.Create()
		table.Geometry("area", "polygon")
		table.SpatialIndex("area")
	})

	if _, err := blueprint.ToSQL(context.Background(), grant()); !errors.Is(err, schema.ErrUnsupported) {
		t.Fatalf("got %v, want an error wrapping schema.ErrUnsupported", err)
	}
}

// TestGrantIsRequired: a blueprint nobody authorized compiles nothing, and the
// refusal names the missing call rather than the SQL.
func TestGrantIsRequired(t *testing.T) {
	blueprint := schema.NewBlueprint(newFake("mysql"), "users", func(table *schema.Blueprint) {
		table.Create()
		table.ID("")
	})

	_, err := blueprint.ToSQL(context.Background(), auth.Grant{})
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("got %v, want auth.ErrForbidden", err)
	}

	if _, err := blueprint.ToSQL(context.Background(), auth.SystemGrant(schema.ActionMigrate, "")); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a system grant with no tenant issued a usable Grant: %v", err)
	}
}

// TestBuildExecutesEveryStatement checks that Build sends what ToSQL compiled,
// in order, and nothing else.
func TestBuildExecutesEveryStatement(t *testing.T) {
	conn := newFake("mysql")
	blueprint := schema.NewBlueprint(conn, "users", func(table *schema.Blueprint) {
		table.Create()
		table.ID("")
		table.String("email", 255).Unique()
	})

	if err := blueprint.Build(context.Background(), grant()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(conn.statements) != 2 {
		t.Fatalf("got %d statements, want 2: %#v", len(conn.statements), conn.statements)
	}
	if !strings.HasPrefix(conn.statements[0], "create table") {
		t.Errorf("first statement is %q", conn.statements[0])
	}
}
