package conformance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/database"
	hsession "github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/support"
)

// sessionPayload is what an application keeps in a session, for the round
// trip: a field of each kind the JSON column has to carry back.
type sessionPayload struct {
	Email string   `json:"email"`
	Roles []string `json:"roles"`
}

// testTheSessionRecordTable is session.DatabaseHandler against a real engine,
// over the table its own migration creates: the record comes back whole, the
// update path and the insert path both write, an expired row reads as
// expired and is pruned, and a sign-out of every other session reaches only
// one subject of one tenant.
//
// The table is created under the suite's prefix by running the migration on a
// connection that carries it, so what is exercised is the migration itself and
// not a copy of its columns.
func testTheSessionRecordTable(t *testing.T, dialect database.Dialect, db *database.DB) {
	ctx := context.Background()
	name := table(hsession.RecordTable)
	drop(t, db, name)
	t.Cleanup(func() { drop(t, db, name) })

	prefixed := database.ForMigrations(database.NewConnection(db.Unwrap(), "", table(""), map[string]any{
		"driver": string(dialect),
		"name":   conformanceConnection,
	}))
	if err := (hsession.CreateSessionRecordsTable{}).Up(ctx, prefixed); err != nil {
		t.Fatalf("the migration does not apply: %v", err)
	}

	h, err := hsession.NewDatabaseHandler[sessionPayload](db, name)
	if err != nil {
		t.Fatalf("NewDatabaseHandler: %v", err)
	}

	confirmed := time.Now().UTC().Truncate(time.Second)
	rec := hsession.Record[sessionPayload]{
		Payload:             sessionPayload{Email: "ana@example.com", Roles: []string{"owner"}},
		Tenant:              "acme",
		SubjectID:           "1",
		Remembered:          true,
		PasswordConfirmedAt: confirmed,
	}
	if err := h.Write(ctx, "s-1", rec, time.Hour); err != nil {
		t.Fatalf("the first write, an insert: %v", err)
	}
	rec.Payload.Roles = append(rec.Payload.Roles, "billing")
	if err := h.Write(ctx, "s-1", rec, time.Hour); err != nil {
		t.Fatalf("the second write, an update: %v", err)
	}
	if err := h.Write(ctx, "s-1", rec, time.Hour); err != nil {
		t.Fatalf("a write of identical values: %v", err)
	}

	got, err := h.Read(ctx, "s-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Payload.Email != rec.Payload.Email || len(got.Payload.Roles) != 2 || got.Tenant != "acme" ||
		got.SubjectID != "1" || !got.Remembered || !got.PasswordConfirmedAt.Equal(confirmed) {
		t.Fatalf("the record came back as %+v, want %+v", got, rec)
	}

	if _, err := h.Read(ctx, "nobody"); !errors.Is(err, hsession.ErrExpired) {
		t.Errorf("a missing id read as %v, want ErrExpired", err)
	}

	// The handler reads the time through support.Now, so the row is expired by
	// travelling past its ttl rather than by sleeping through it.
	previous := support.GetTestNow()
	support.FreezeTime()
	t.Cleanup(func() { support.SetTestNow(previous) })
	if err := h.Write(ctx, "s-short", rec, time.Minute); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := h.Read(ctx, "s-short"); err != nil {
		t.Fatalf("a row read before its ttl ran out: %v", err)
	}
	support.Travel(2 * time.Minute)
	if _, err := h.Read(ctx, "s-short"); !errors.Is(err, hsession.ErrExpired) {
		t.Errorf("an expired row read as %v, want ErrExpired", err)
	}
	if pruned, err := h.Prune(ctx); err != nil || pruned != 1 {
		t.Errorf("Prune = %d, %v, want the one expired row", pruned, err)
	}

	for _, other := range []struct {
		id, tenant, subject string
	}{
		{"s-2", "acme", "1"},
		{"s-3", "acme", "2"},
		{"s-4", "globex", "1"},
	} {
		r := rec
		r.Tenant, r.SubjectID = other.tenant, other.subject
		if err := h.Write(ctx, other.id, r, time.Hour); err != nil {
			t.Fatalf("write %s: %v", other.id, err)
		}
	}
	if err := h.DestroyIndex(ctx, "acme", "1", "s-1"); err != nil {
		t.Fatalf("DestroyIndex: %v", err)
	}
	for id, live := range map[string]bool{"s-1": true, "s-2": false, "s-3": true, "s-4": true} {
		_, err := h.Read(ctx, id)
		if live && err != nil {
			t.Errorf("%s was signed out by a sign-out that was not its subject's: %v", id, err)
		}
		if !live && !errors.Is(err, hsession.ErrExpired) {
			t.Errorf("%s survived the sign-out of its subject: %v", id, err)
		}
	}

	if err := h.Destroy(ctx, "s-1"); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, err := h.Read(ctx, "s-1"); !errors.Is(err, hsession.ErrExpired) {
		t.Errorf("a destroyed session read as %v, want ErrExpired", err)
	}
}
