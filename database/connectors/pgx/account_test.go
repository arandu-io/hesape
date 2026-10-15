package pgx_test

// auth/account on PostgreSQL: the contract suite against the skeleton's users
// table, model and policy, in a schema made for each subtest and dropped
// after it. It skips without ARANDU_TEST_POSTGRES_DSN, like the conformance
// suite.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"testing"

	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/auth/account/accounttest"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/events"
	jackpgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// accountServer opens a schema of its own with the outbox, the users table
// and the second factor's tables in it, and drops it when the test ends.
func accountServer(t *testing.T) *database.DB {
	t.Helper()
	if dsn() == "" {
		t.Skip("no DSN: set ARANDU_TEST_POSTGRES_DSN to run this against a real server")
	}
	ctx := context.Background()
	admin, err := sql.Open(driverName(t), dsn())
	if err != nil {
		t.Fatal(err)
	}
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	schema := "arandu_account_" + hex.EncodeToString(suffix)
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		_ = admin.Close()
		t.Fatalf("creating the schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Errorf("dropping the schema %s: %v", schema, err)
		}
		_ = admin.Close()
	})

	cfg, err := jackpgx.ParseConfig(dsn())
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeParams["search_path"] = schema
	raw := stdlib.OpenDB(*cfg)
	raw.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = raw.Close() })

	conn := database.ForMigrations(database.NewConnection(raw, cfg.Database, "", map[string]any{
		"driver": string(database.DialectPostgres), "name": "pgsql",
	}))
	all := append(events.NewModule().Migrations(), accountUserMigrations()...)
	for _, m := range append(all, account.Migrations()...) {
		if err := m.Up(ctx, conn); err != nil {
			t.Fatalf("applying %s: %v", m.GetName(), err)
		}
	}
	return database.Wrap(raw, database.DialectPostgres)
}

func TestTheAccountContract(t *testing.T) {
	if dsn() == "" {
		t.Skip("no DSN: set ARANDU_TEST_POSTGRES_DSN to run this against a real server")
	}
	accounttest.Run(t, func(t *testing.T) accounttest.Fixture {
		db := accountServer(t)
		return accounttest.Fixture{
			DB:     db,
			Query:  func() *model.Builder { return accountUsers(db) },
			Policy: account.PolicyOf(accountPolicy{}),
		}
	})
}
