package migrations_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/console"
	migrationcommands "github.com/arandu-io/hesape/database/console/migrations"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
)

// rollbackGroup keeps the migrations below out of the default registry group,
// so a rollback in this test can only reach the two it registered.
const rollbackGroup = "rollback_command"

// backfillMigration has an Up, no Down, and says why there can never be one.
type backfillMigration struct {
	migrations.BaseMigration
	name string
}

func (m backfillMigration) GetName() string { return m.name }

func (m backfillMigration) Up(context.Context, migrations.Connection) error { return nil }

func (m backfillMigration) Irreversible() string {
	return "the rows it filled in are no longer distinguishable from the rows it did not"
}

// indexMigration is an ordinary migration, with both directions.
type indexMigration struct {
	migrations.BaseMigration
	name  string
	downs *[]string
}

func (m indexMigration) GetName() string { return m.name }

func (m indexMigration) Up(context.Context, migrations.Connection) error { return nil }

func (m indexMigration) Down(context.Context, migrations.Connection) error {
	*m.downs = append(*m.downs, m.name)
	return nil
}

// testConnection is a Connection for migrations that write no DDL. This test is
// about what the command prints, so nothing here has to reach a server.
type testConnection struct{}

func (testConnection) GetName() string { return "testing" }

func (testConnection) Schema() *schema.Builder {
	panic("these migrations write no DDL, so nothing should ask this connection for a schema builder")
}

func (testConnection) Statement(context.Context, string, []any) (bool, error) { return true, nil }

func (testConnection) Select(context.Context, string, []any) ([]map[string]any, error) {
	return nil, nil
}

// testResolver answers one connection under every name.
type testResolver struct{ defaultName string }

func (r *testResolver) Connection(string) (migrations.Connection, error) {
	return testConnection{}, nil
}

func (r *testResolver) GetDefaultConnection() string     { return r.defaultName }
func (r *testResolver) SetDefaultConnection(name string) { r.defaultName = name }

// testRepository is a MigrationRepositoryInterface backed by a slice, which is
// the whole of the database this test needs.
type testRepository struct {
	records []migrations.MigrationRecord
	exists  bool
}

func (r *testRepository) GetRan(context.Context) ([]string, error) {
	out := make([]string, 0, len(r.records))
	for _, record := range r.records {
		out = append(out, record.Migration)
	}
	return out, nil
}

func (r *testRepository) GetMigrations(_ context.Context, steps int) ([]migrations.MigrationRecord, error) {
	out := newestFirst(r.records)
	if len(out) > steps {
		out = out[:steps]
	}
	return out, nil
}

func (r *testRepository) GetMigrationsByBatch(_ context.Context, batch int) ([]migrations.MigrationRecord, error) {
	var out []migrations.MigrationRecord
	for _, record := range r.records {
		if record.Batch == batch {
			out = append(out, record)
		}
	}
	return newestFirst(out), nil
}

func (r *testRepository) GetLast(ctx context.Context) ([]migrations.MigrationRecord, error) {
	last := 0
	for _, record := range r.records {
		if record.Batch > last {
			last = record.Batch
		}
	}
	return r.GetMigrationsByBatch(ctx, last)
}

func (r *testRepository) GetMigrationBatches(context.Context) (map[string]int, error) {
	out := map[string]int{}
	for _, record := range r.records {
		out[record.Migration] = record.Batch
	}
	return out, nil
}

func (r *testRepository) Log(_ context.Context, file string, batch int) error {
	r.records = append(r.records, migrations.MigrationRecord{
		ID:        len(r.records) + 1,
		Migration: file,
		Batch:     batch,
	})
	return nil
}

func (r *testRepository) Delete(_ context.Context, migration migrations.MigrationRecord) error {
	var kept []migrations.MigrationRecord
	for _, record := range r.records {
		if record.Migration != migration.Migration {
			kept = append(kept, record)
		}
	}
	r.records = kept
	return nil
}

func (r *testRepository) GetNextBatchNumber(context.Context) (int, error) {
	last := 0
	for _, record := range r.records {
		if record.Batch > last {
			last = record.Batch
		}
	}
	return last + 1, nil
}

func (r *testRepository) CreateRepository(context.Context) error { r.exists = true; return nil }
func (r *testRepository) RepositoryExists(context.Context) bool  { return r.exists }
func (r *testRepository) DeleteRepository(context.Context) error { r.exists = false; return nil }
func (r *testRepository) SetSource(string)                       {}

// newestFirst copies records into the order a rollback undoes them in. The name
// carries the order, so the newest is the greatest.
func newestFirst(records []migrations.MigrationRecord) []migrations.MigrationRecord {
	out := append([]migrations.MigrationRecord(nil), records...)
	sort.Slice(out, func(i, j int) bool { return out[i].Migration > out[j].Migration })
	return out
}

// reportedReverted reports whether the command printed its REVERTED row for
// name. The row is the migration's name, a run of dots, and the word, so a
// prefix and a suffix identify it without depending on the width.
func reportedReverted(printed, name string) bool {
	for _, line := range strings.Split(printed, "\n") {
		if strings.HasPrefix(line, name+" ") && strings.HasSuffix(line, "REVERTED") {
			return true
		}
	}
	return false
}

// TestRollbackCommandDoesNotReportAMigrationItSkippedAsReverted is the command's
// half of the same question the migrator's own tests ask.
//
// The command prints one row per name the migrator answers with, so a name for a
// migration that was left applied becomes a REVERTED row on the screen -- printed
// directly under the line saying the migration was skipped and why. The two lines
// contradict each other, and the row is the one an operator reads as the result.
func TestRollbackCommandDoesNotReportAMigrationItSkippedAsReverted(t *testing.T) {
	var downs []string
	migrations.Register(backfillMigration{name: "2026_01_01_000000_backfill_totals"}, rollbackGroup)
	migrations.Register(indexMigration{name: "2026_01_02_000000_add_index", downs: &downs}, rollbackGroup)

	repository := &testRepository{exists: true}
	migrator := migrations.NewMigrator(repository, &testResolver{}, nil)

	if _, err := migrator.Run(context.Background(), []string{rollbackGroup}, migrations.Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out, errOut := &strings.Builder{}, &strings.Builder{}
	command := migrationcommands.RollbackCommand(migrationcommands.Deps{
		Migrator:        migrator,
		MigrationGroups: []string{rollbackGroup},
	})

	if err := command.Run(context.Background(), console.NewIO("migrate:rollback", nil, out, errOut, nil)); err != nil {
		t.Fatalf("migrate:rollback: %v", err)
	}

	// The migrator writes its progress to the flag set's output, and the rows to
	// the standard one. Both are what a person sees.
	printed := out.String() + errOut.String()

	if !strings.Contains(printed, "2026_01_01_000000_backfill_totals Skipped:") {
		t.Fatalf("the command never said it skipped the backfill:\n%s", printed)
	}
	if reportedReverted(printed, "2026_01_01_000000_backfill_totals") {
		t.Fatalf("the command reported REVERTED for the migration it had just said it skipped:\n%s", printed)
	}
	if !reportedReverted(printed, "2026_01_02_000000_add_index") {
		t.Fatalf("the command did not report the migration it undid:\n%s", printed)
	}

	if strings.Join(downs, ",") != "2026_01_02_000000_add_index" {
		t.Fatalf("Down ran for %v, want only the reversible migration", downs)
	}
	if len(repository.records) != 1 || repository.records[0].Migration != "2026_01_01_000000_backfill_totals" {
		t.Fatalf("the repository holds %v, and the backfill is still applied", repository.records)
	}
}
