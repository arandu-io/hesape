package session_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/session"
)

// The handler's SQL is exercised against SQLite, PostgreSQL and MySQL by
// database/conformance, which every connector runs. What is here is what it
// refuses before a statement is built.

func TestTheRecordDatabaseHandlerRefusesATableNameThatIsNotOne(t *testing.T) {
	for _, bad := range []string{"session_records; DROP TABLE users", `sessions"`, "session-records"} {
		if _, err := session.NewDatabaseHandler[string](nil, bad); !errors.Is(err, session.ErrBadRecordTable) {
			t.Fatalf("%q was accepted as a table name: %v", bad, err)
		}
	}
	if _, err := session.NewDatabaseHandler[string](nil, ""); err != nil {
		t.Fatalf("the empty name is RecordTable, and it was refused: %v", err)
	}
}

// TestTheDatabaseHandlerRefusesASubjectlessSignOut: the empty subject id of a
// tenant is every guest of it, so the refusal ArrayHandler makes is made here
// too -- before any statement, which is why a nil connection is enough.
func TestTheDatabaseHandlerRefusesASubjectlessSignOut(t *testing.T) {
	h, err := session.NewDatabaseHandler[string](nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range [][2]string{{"", "1"}, {"acme", ""}} {
		if err := h.DestroyIndex(context.Background(), scope[0], scope[1], ""); err == nil {
			t.Errorf("DestroyIndex(%q, %q) was accepted", scope[0], scope[1])
		}
	}
}

// TestTheDatabaseHandlerRefusesARowThatNeverExpires: a ttl of zero would be a
// row every read treats as expired, and a negative one a session nobody can
// keep -- either way the caller has a bug worth hearing about.
func TestTheDatabaseHandlerRefusesARowThatNeverExpires(t *testing.T) {
	h, err := session.NewDatabaseHandler[string](nil, "")
	if err != nil {
		t.Fatal(err)
	}
	err = h.Write(context.Background(), "s-1", session.Record[string]{}, 0)
	if err == nil || !strings.Contains(err.Error(), "lifetime") {
		t.Fatalf("err = %v, want the refusal of a session with no lifetime", err)
	}
}

// TestTheRecordTableMigrationIsListed: an application wires the table by the
// list, so a migration missing from it is a table nobody creates.
func TestTheRecordTableMigrationIsListed(t *testing.T) {
	listed := session.RecordMigrations()
	if len(listed) != 1 || listed[0].GetName() != (session.CreateSessionRecordsTable{}).GetName() {
		t.Fatalf("RecordMigrations() = %v", listed)
	}
}
