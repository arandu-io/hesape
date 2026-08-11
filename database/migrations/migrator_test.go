package migrations

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeMigration is a migration that records what was asked of it.
type fakeMigration struct {
	BaseMigration
	name    string
	ups     *[]string
	downs   *[]string
	upErr   error
	skipped bool
}

func (m fakeMigration) GetName() string { return m.name }

func (m fakeMigration) ShouldRun() bool { return !m.skipped }

func (m fakeMigration) Up(context.Context, Connection) error {
	if m.upErr != nil {
		return m.upErr
	}
	*m.ups = append(*m.ups, m.name)
	return nil
}

func (m fakeMigration) Down(context.Context, Connection) error {
	*m.downs = append(*m.downs, m.name)
	return nil
}

// fakeRepository is a MigrationRepositoryInterface backed by a slice.
type fakeRepository struct {
	records []MigrationRecord
	exists  bool
	source  string
}

func (r *fakeRepository) GetRan(context.Context) ([]string, error) {
	out := make([]string, 0, len(r.records))
	for _, record := range r.records {
		out = append(out, record.Migration)
	}
	return out, nil
}

func (r *fakeRepository) GetMigrations(_ context.Context, steps int) ([]MigrationRecord, error) {
	out := append([]MigrationRecord(nil), r.records...)
	sortRecordsByName(out, true)
	if len(out) > steps {
		out = out[:steps]
	}
	return out, nil
}

func (r *fakeRepository) GetMigrationsByBatch(_ context.Context, batch int) ([]MigrationRecord, error) {
	var out []MigrationRecord
	for _, record := range r.records {
		if record.Batch == batch {
			out = append(out, record)
		}
	}
	sortRecordsByName(out, true)
	return out, nil
}

func (r *fakeRepository) GetLast(ctx context.Context) ([]MigrationRecord, error) {
	last := 0
	for _, record := range r.records {
		if record.Batch > last {
			last = record.Batch
		}
	}
	return r.GetMigrationsByBatch(ctx, last)
}

func (r *fakeRepository) GetMigrationBatches(context.Context) (map[string]int, error) {
	out := map[string]int{}
	for _, record := range r.records {
		out[record.Migration] = record.Batch
	}
	return out, nil
}

func (r *fakeRepository) Log(_ context.Context, file string, batch int) error {
	r.records = append(r.records, MigrationRecord{ID: len(r.records) + 1, Migration: file, Batch: batch})
	return nil
}

func (r *fakeRepository) Delete(_ context.Context, migration MigrationRecord) error {
	var kept []MigrationRecord
	for _, record := range r.records {
		if record.Migration != migration.Migration {
			kept = append(kept, record)
		}
	}
	r.records = kept
	return nil
}

func (r *fakeRepository) GetNextBatchNumber(context.Context) (int, error) {
	last := 0
	for _, record := range r.records {
		if record.Batch > last {
			last = record.Batch
		}
	}
	return last + 1, nil
}

func (r *fakeRepository) CreateRepository(context.Context) error { r.exists = true; return nil }
func (r *fakeRepository) RepositoryExists(context.Context) bool  { return r.exists }
func (r *fakeRepository) DeleteRepository(context.Context) error { r.exists = false; return nil }
func (r *fakeRepository) SetSource(name string)                  { r.source = name }

// fakeConnection is a Connection that records the statements it was given.
type fakeConnection struct{ statements []string }

func (c *fakeConnection) GetName() string { return "testing" }

func (c *fakeConnection) Statement(_ context.Context, query string, _ []any) (bool, error) {
	c.statements = append(c.statements, query)
	return true, nil
}

func (c *fakeConnection) Select(context.Context, string, []any) ([]map[string]any, error) {
	return nil, nil
}

// fakeResolver answers one connection under every name.
type fakeResolver struct {
	connection  *fakeConnection
	defaultName string
}

func (r *fakeResolver) Connection(string) (Connection, error) { return r.connection, nil }
func (r *fakeResolver) GetDefaultConnection() string          { return r.defaultName }
func (r *fakeResolver) SetDefaultConnection(name string)      { r.defaultName = name }

func newTestMigrator() (*Migrator, *fakeRepository, *fakeConnection) {
	repository := &fakeRepository{exists: true}
	connection := &fakeConnection{}
	resolver := &fakeResolver{connection: connection}
	return NewMigrator(repository, resolver, nil), repository, connection
}

func TestRegisteredSortsByName(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups []string
	Register(fakeMigration{name: "2026_01_02_000000_second", ups: &ups})
	Register(fakeMigration{name: "2026_01_01_000000_first", ups: &ups})

	got := Registered()
	if len(got) != 2 {
		t.Fatalf("Registered answered %d migrations, want 2", len(got))
	}
	if got[0].GetName() != "2026_01_01_000000_first" {
		t.Fatalf("Registered answered %q first, and the date prefix is what carries the order", got[0].GetName())
	}
}

func TestRegisterRefusesTheSameNameTwice(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups []string
	Register(fakeMigration{name: "2026_01_01_000000_first", ups: &ups})

	defer func() {
		if recover() == nil {
			t.Fatal("registering two migrations under one name did not panic, so one of them would apply and the other be recorded")
		}
	}()
	Register(fakeMigration{name: "2026_01_01_000000_first", ups: &ups, skipped: true})
}

func TestRunAppliesPendingInOrderAndRecordsThem(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups, downs []string
	Register(fakeMigration{name: "2026_01_02_000000_second", ups: &ups, downs: &downs})
	Register(fakeMigration{name: "2026_01_01_000000_first", ups: &ups, downs: &downs})

	migrator, repository, _ := newTestMigrator()

	ran, err := migrator.Run(context.Background(), nil, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Join(ran, ",") != "2026_01_01_000000_first,2026_01_02_000000_second" {
		t.Fatalf("Run applied %v, want the date order", ran)
	}
	if strings.Join(ups, ",") != strings.Join(ran, ",") {
		t.Fatalf("Up was called for %v but Run reported %v", ups, ran)
	}
	if len(repository.records) != 2 {
		t.Fatalf("the repository recorded %d migrations, want 2", len(repository.records))
	}
	if repository.records[0].Batch != 1 || repository.records[1].Batch != 1 {
		t.Fatal("one Run is one batch, which is what makes a rollback undo a deploy")
	}

	// A second run applies nothing.
	again, err := migrator.Run(context.Background(), nil, Options{})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("the second Run applied %v, and everything was already applied", again)
	}
}

func TestRunStopsAtTheFirstFailureAndDoesNotRecordIt(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups, downs []string
	boom := errors.New("column already exists")
	Register(fakeMigration{name: "2026_01_01_000000_first", ups: &ups, downs: &downs})
	Register(fakeMigration{name: "2026_01_02_000000_second", ups: &ups, downs: &downs, upErr: boom})
	Register(fakeMigration{name: "2026_01_03_000000_third", ups: &ups, downs: &downs})

	migrator, repository, _ := newTestMigrator()

	if _, err := migrator.Run(context.Background(), nil, Options{}); !errors.Is(err, boom) {
		t.Fatalf("Run answered %v, want the migration's own error", err)
	}
	if len(ups) != 1 {
		t.Fatalf("Up ran for %v, and it should have stopped at the failure", ups)
	}
	if len(repository.records) != 1 || repository.records[0].Migration != "2026_01_01_000000_first" {
		t.Fatalf("the repository holds %v, and a migration that failed must not be recorded", repository.records)
	}
}

func TestSkippedMigrationIsNotRecorded(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups, downs []string
	Register(fakeMigration{name: "2026_01_01_000000_first", ups: &ups, downs: &downs, skipped: true})

	migrator, repository, _ := newTestMigrator()

	if _, err := migrator.Run(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ups) != 0 {
		t.Fatal("a migration whose ShouldRun is false was run anyway")
	}
	if len(repository.records) != 0 {
		t.Fatal("a skipped migration was recorded, so it would never be reconsidered")
	}
}

func TestRollbackUndoesTheLastBatchNewestFirst(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups, downs []string
	Register(fakeMigration{name: "2026_01_01_000000_first", ups: &ups, downs: &downs})
	Register(fakeMigration{name: "2026_01_02_000000_second", ups: &ups, downs: &downs})

	migrator, repository, _ := newTestMigrator()

	if _, err := migrator.Run(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reverted, err := migrator.Rollback(context.Background(), nil, Options{})
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if strings.Join(reverted, ",") != "2026_01_02_000000_second,2026_01_01_000000_first" {
		t.Fatalf("Rollback reverted %v, and the last thing applied is the first thing undone", reverted)
	}
	if len(repository.records) != 0 {
		t.Fatalf("the repository still holds %v after a full rollback", repository.records)
	}
}

func TestWithoutMigrationsLeavesThemPending(t *testing.T) {
	t.Cleanup(func() { flushRegistry(); WithoutMigrations(nil) })
	flushRegistry()

	var ups, downs []string
	Register(fakeMigration{name: "2026_01_01_000000_first", ups: &ups, downs: &downs})
	WithoutMigrations([]string{"2026_01_01_000000_first"})

	migrator, _, _ := newTestMigrator()

	ran, err := migrator.Run(context.Background(), nil, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ran) != 0 {
		t.Fatalf("Run applied %v, and it was told to skip it", ran)
	}
}

func TestGetMigrationNameReadsAPath(t *testing.T) {
	migrator, _, _ := newTestMigrator()

	for path, want := range map[string]string{
		"database/migrations/2026_01_01_000000_first.go": "2026_01_01_000000_first",
		"2026_01_01_000000_first":                        "2026_01_01_000000_first",
		"/tmp/2014_10_12_000000_create_users_table.php":  "2014_10_12_000000_create_users_table",
	} {
		if got := migrator.GetMigrationName(path); got != want {
			t.Fatalf("GetMigrationName(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestBaseMigrationDefaultsMatchThePHP(t *testing.T) {
	var m BaseMigration

	if !m.ShouldRun() {
		t.Fatal("Migration::shouldRun is true unless a migration says otherwise")
	}
	if !m.WithinTransaction() {
		t.Fatal("Migration::$withinTransaction defaults to true, and a zero-valued struct must agree")
	}
	if (BaseMigration{OutsideTransaction: true}).WithinTransaction() {
		t.Fatal("OutsideTransaction did not turn the wrapping off")
	}
}
