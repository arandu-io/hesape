package conformance

import (
	"context"
	"fmt"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
)

// testThePresenceCount is the count `unique` and `exists` ask, against a real
// engine: the distinct aggregate compiles and scans on every dialect, the
// tenant on the Grant filters a tenant's table, a shared table is counted
// whole, and the ignored row and the extra conditions narrow the count.
func testThePresenceCount(t *testing.T, db *database.DB) {
	ctx := context.Background()
	members, accounts := table("presence_members"), table("presence_accounts")
	for _, name := range []string{members, accounts} {
		drop(t, db, name)
		t.Cleanup(func() { drop(t, db, name) })
	}

	statements := []string{
		fmt.Sprintf(`CREATE TABLE %s (id %s PRIMARY KEY, tenant_id %s NOT NULL, email %s NOT NULL, status %s NOT NULL)`,
			members, database.KeyText, database.KeyText, database.KeyText, database.KeyText),
		fmt.Sprintf(`CREATE TABLE %s (id %s PRIMARY KEY, email %s NOT NULL)`,
			accounts, database.KeyText, database.KeyText),
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	for _, row := range [][4]string{
		{"1", "acme", "ana@example.com", "active"},
		{"2", "acme", "bia@example.com", "banned"},
		{"3", "globex", "caio@example.com", "active"},
	} {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id, tenant_id, email, status) VALUES (?, ?, ?, ?)`, members),
			row[0], row[1], row[2], row[3]); err != nil {
			t.Fatalf("insert member %s: %v", row[0], err)
		}
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id, email) VALUES (?, ?)`, accounts), "1", "caio@example.com"); err != nil {
		t.Fatalf("insert account: %v", err)
	}

	presence := database.NewPresence(db, accounts)
	acme := auth.SystemGrant("member.create", "acme")

	cases := []struct {
		name      string
		table     string
		values    []any
		excludeID any
		extra     map[string]string
		want      int
	}{
		{"a value the tenant holds", members, []any{"ana@example.com"}, nil, nil, 1},
		{"a value only another tenant holds", members, []any{"caio@example.com"}, nil, nil, 0},
		{"a list, counted by distinct value", members, []any{"ana@example.com", "bia@example.com", "nobody@example.com"}, nil, nil, 2},
		{"the row being edited, ignored", members, []any{"ana@example.com"}, "1", nil, 0},
		{"an extra condition", members, []any{"ana@example.com", "bia@example.com"}, nil, map[string]string{"status": "!banned"}, 1},
		{"a shared table, counted whole", accounts, []any{"caio@example.com"}, nil, nil, 1},
	}
	for _, c := range cases {
		got, err := presence.Count(ctx, acme, c.table, "email", c.values, c.excludeID, "", c.extra)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: count = %d, want %d", c.name, got, c.want)
		}
	}
}
