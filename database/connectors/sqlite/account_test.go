package sqlite_test

// auth/account on SQLite: the contract suite against the skeleton's users
// table, model and policy; the second factor's migration held to the schema
// the skeleton's own migration made, since every enrolled account is a row
// of it; and Draft.Fill writing the application's own column.

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/auth/account/accounttest"
	"github.com/arandu-io/hesape/database"
	sqliteconnector "github.com/arandu-io/hesape/database/connectors/sqlite"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/schema"
	"github.com/arandu-io/hesape/events"
)

// accountDatabase is a file database with the outbox, the users table and the
// given second-factor migrations applied. One connection, so concurrent
// transactions queue rather than meet SQLite's single writer.
func accountDatabase(t *testing.T, twoFactor []migrations.Migration) (*database.DB, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "account.sqlite")
	handle, err := sql.Open(sqliteconnector.SQLiteConnector{}.DriverName(), "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	handle.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = handle.Close() })
	conn := database.ForMigrations(database.NewConnection(handle, path, "", map[string]any{
		"driver": string(database.DialectSQLite), "name": "account",
	}))
	all := append(events.NewModule().Migrations(), accountUserMigrations()...)
	for _, m := range append(all, twoFactor...) {
		if err := m.Up(context.Background(), conn); err != nil {
			t.Fatalf("applying %s: %v", m.GetName(), err)
		}
	}
	return database.Wrap(handle, database.DialectSQLite), handle
}

func TestTheAccountContract(t *testing.T) {
	accounttest.Run(t, func(t *testing.T) accounttest.Fixture {
		db, _ := accountDatabase(t, account.Migrations())
		return accounttest.Fixture{
			DB:     db,
			Query:  func() *model.Builder { return accountUsers(db) },
			Policy: account.PolicyOf(accountPolicy{}),
		}
	})
}

// TestTheTwoFactorMigrationIsTheOneApplicationsRan: an application that moves
// to the package keeps its tables, and the package's migration declares the
// same name, so the two must make the same schema.
func TestTheTwoFactorMigrationIsTheOneApplicationsRan(t *testing.T) {
	_, mine := accountDatabase(t, account.Migrations())
	_, theirs := accountDatabase(t, []migrations.Migration{skeletonCreateTwoFactor{}})
	read := func(h *sql.DB) map[string]string {
		rows, err := h.Query(`SELECT name, sql FROM sqlite_master WHERE tbl_name IN ('user_two_factor', 'user_recovery_codes') ORDER BY name`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]string{}
		for rows.Next() {
			var name string
			var stmt sql.NullString
			if err := rows.Scan(&name, &stmt); err != nil {
				t.Fatal(err)
			}
			out[name] = stmt.String
		}
		return out
	}
	got, want := read(mine), read(theirs)
	if len(want) == 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("the package's migration made\n%v\nthe applications' made\n%v", got, want)
	}
	// The create keeps the name applications recorded; what follows it -- the
	// widening of its instants to microseconds -- is a migration of its own.
	if m := account.Migrations(); len(m) == 0 || m[0].GetName() != (skeletonCreateTwoFactor{}).GetName() {
		t.Fatalf("Migrations() = %v, want the name applications recorded first", m)
	}
}

// TestDraftFillWritesTheApplicationsColumns: the roles column is the
// application's, written by Fill when provisioning and left empty by a guest
// registration, which may not carry one.
func TestDraftFillWritesTheApplicationsColumns(t *testing.T) {
	db, handle := accountDatabase(t, account.Migrations())
	cfg := account.Config{DB: db, Query: func() *model.Builder { return accountUsers(db) }, Policy: account.PolicyOf(accountPolicy{})}
	users, err := account.NewUsers(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin, err := users.Provision(ctx, "tenant-a", account.Draft{
		Email: "root@example.test", Password: "a-long-enough-password", Verified: true,
		Fill: func(a account.Account) error { a.(*accountUser).Roles = accountRoles{accountAdmin}; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := handle.QueryRow(`SELECT roles FROM users WHERE id = ?`, admin.AccountID()).Scan(&stored); err != nil || stored != `["admin"]` {
		t.Fatalf("the roles column holds %q (%v), want the JSON array", stored, err)
	}
	found, err := users.Lookup(ctx, "tenant-a", "root@example.test")
	if err != nil || !found.(*accountUser).subject().HasRole(accountAdmin) {
		t.Fatalf("the provisioned administrator read back as %v (%v)", found, err)
	}
	member, err := users.Register(ctx, "tenant-a", account.Registration{Name: "Ana", Email: "ana@example.test", Password: "a-long-enough-password"})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.QueryRow(`SELECT roles FROM users WHERE id = ?`, member.AccountID()).Scan(&stored); err != nil || stored != "[]" {
		t.Fatalf("a member's roles column holds %q (%v), want []", stored, err)
	}
	cfg.Hooks.Prepare = func(_ context.Context, _ *account.Registration, a account.Account) error {
		a.(*accountUser).Roles = accountRoles{accountAdmin}
		return nil
	}
	escalating, _ := account.NewUsers(cfg)
	if _, err := escalating.Register(ctx, "tenant-a", account.Registration{Name: "M", Email: "m@example.test", Password: "a-long-enough-password"}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a guest registration carrying a role answered %v, want auth.ErrForbidden", err)
	}
}

// skeletonCreateTwoFactor is the migration applications ran before the
// package declared it, verbatim.
type skeletonCreateTwoFactor struct{ migrations.BaseMigration }

func (skeletonCreateTwoFactor) GetName() string { return "20260828_0003_create_two_factor" }
func (skeletonCreateTwoFactor) Up(ctx context.Context, conn migrations.Connection) error {
	if err := conn.Schema().Create(ctx, "user_two_factor", func(table *schema.Blueprint) {
		table.String("user_id").Primary()
		table.String("tenant_id")
		table.Text("secret")
		table.Timestamp("confirmed_at").Nullable()
		table.BigInteger("last_used_step")
		table.Timestamp("created_at")
	}); err != nil {
		return err
	}
	return conn.Schema().Create(ctx, "user_recovery_codes", func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.String("tenant_id")
		table.String("user_id")
		table.Text("code_hash")
		table.Timestamp("used_at").Nullable()
		table.Timestamp("created_at")
		table.Index([]string{"tenant_id", "user_id"}, "user_recovery_codes_owner_idx")
	})
}
