package mysql_test

// auth/account on MySQL: the contract suite against the skeleton's users
// table, model and policy, in a database made for each subtest and dropped
// after it. It skips without ARANDU_TEST_MYSQL_DSN, like the conformance
// suite.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/auth/account/accounttest"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/events"
	mysqldriver "github.com/go-sql-driver/mysql"
)

// accountServer opens a database of its own with the outbox, the users table
// and the second factor's tables in it, and drops it when the test ends.
func accountServer(t *testing.T) *database.DB {
	t.Helper()
	if dsn() == "" {
		t.Skip("no DSN: set ARANDU_TEST_MYSQL_DSN to run this against a real server")
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
	name := "arandu_account_" + hex.EncodeToString(suffix)
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		_ = admin.Close()
		t.Fatalf("creating the database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), "DROP DATABASE "+name); err != nil {
			t.Errorf("dropping the database %s: %v", name, err)
		}
		_ = admin.Close()
	})

	cfg, err := mysqldriver.ParseDSN(dsn())
	if err != nil {
		t.Fatal(err)
	}
	// The models scan DATETIME columns into time.Time, in UTC.
	cfg.DBName, cfg.ParseTime, cfg.Loc = name, true, time.UTC
	raw, err := sql.Open(driverName(t), cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = raw.Close() })

	conn := database.ForMigrations(database.NewConnection(raw, name, "", map[string]any{
		"driver": string(database.DialectMySQL), "name": "mysql",
	}))
	all := append(events.NewModule().Migrations(), accountUserMigrations()...)
	for _, m := range append(all, account.Migrations()...) {
		if err := m.Up(ctx, conn); err != nil {
			t.Fatalf("applying %s: %v", m.GetName(), err)
		}
	}
	return database.Wrap(raw, database.DialectMySQL)
}

func TestTheAccountContract(t *testing.T) {
	if dsn() == "" {
		t.Skip("no DSN: set ARANDU_TEST_MYSQL_DSN to run this against a real server")
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
