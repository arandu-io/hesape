package migrations

import (
	"context"
	"errors"
	"github.com/arandu-io/hesape/database/schema"
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

// oneWayMigration has an Up and no Down, and declares nothing about being
// reversible. It is the shape a rollback refuses.
type oneWayMigration struct {
	BaseMigration
	name string
	ups  *[]string
}

func (m oneWayMigration) GetName() string { return m.name }

func (m oneWayMigration) Up(context.Context, Connection) error {
	*m.ups = append(*m.ups, m.name)
	return nil
}

// backfillMigration has an Up, no Down, and says why there can never be one.
type backfillMigration struct {
	oneWayMigration
}

func (m backfillMigration) Irreversible() string {
	return "the rows it filled in are no longer distinguishable from the rows it did not"
}

// contradictoryMigration declares both a Down and Irreversible, which are
// opposite claims about the same migration.
type contradictoryMigration struct {
	fakeMigration
}

func (m contradictoryMigration) Irreversible() string { return "nothing undoes this" }

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

// Schema answers a builder that records rather than executing. The migrator
// tests are about order, batches and rollback, and none of them needs a server
// -- but every migration now reaches for this, so it has to answer something
// that compiles a statement without sending one.
func (c *fakeConnection) Schema() *schema.Builder {
	return schema.NewBuilder(&recordingSchemaConnection{recorder: &recordingConnection{name: "testing"}})
}

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

func TestRollbackRefusesAMigrationThatDeclaresNoDown(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups []string
	Register(oneWayMigration{name: "2026_01_01_000000_add_column", ups: &ups})

	migrator, repository, _ := newTestMigrator()

	if _, err := migrator.Run(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	_, err := migrator.Rollback(context.Background(), nil, Options{})
	if err == nil {
		t.Fatal("Rollback reported success for a migration it did not undo")
	}
	if !strings.Contains(err.Error(), "neither Down nor Irreversible") {
		t.Fatalf("Rollback: %v, and the error has to name both ways out", err)
	}

	// The schema was not touched, so the row saying it is applied is still
	// true. Deleting it would send the next Run through Up a second time.
	if len(repository.records) != 1 {
		t.Fatalf("the repository holds %v, and a change nobody undid keeps its record", repository.records)
	}
}

func TestRollbackLeavesADeclaredIrreversibleMigrationApplied(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups, downs []string
	Register(backfillMigration{oneWayMigration{name: "2026_01_01_000000_backfill_totals", ups: &ups}})
	Register(fakeMigration{name: "2026_01_02_000000_add_index", ups: &ups, downs: &downs})

	migrator, repository, _ := newTestMigrator()

	if _, err := migrator.Run(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := migrator.Rollback(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Rollback: %v, and a declared irreversible migration must not stop the batch", err)
	}

	// The reversible one in the same batch was undone, which is the point of
	// declaring: the rollback carries on instead of refusing.
	if strings.Join(downs, ",") != "2026_01_02_000000_add_index" {
		t.Fatalf("the rollback ran Down for %v, want only the reversible migration", downs)
	}
	if len(repository.records) != 1 || repository.records[0].Migration != "2026_01_01_000000_backfill_totals" {
		t.Fatalf("the repository holds %v, and the backfill is still applied", repository.records)
	}
}

// TestRollbackDoesNotReportAnIrreversibleMigrationAsReverted reads the two
// things the test above does not: the slice Rollback answers and the text it
// wrote.
//
// The state was already right -- the schema keeps the change and the repository
// keeps the row -- while the returned names included the migration anyway. The
// rollback command prints one line per name in that slice, so a single run said
// "Skipped" and then "REVERTED" about the same migration, and anything reading
// the slice rather than the screen had no way to tell it was left applied.
func TestRollbackDoesNotReportAnIrreversibleMigrationAsReverted(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups, downs []string
	Register(backfillMigration{oneWayMigration{name: "2026_01_01_000000_backfill_totals", ups: &ups}})
	Register(fakeMigration{name: "2026_01_02_000000_add_index", ups: &ups, downs: &downs})

	migrator, _, _ := newTestMigrator()
	output := &strings.Builder{}
	migrator.SetOutput(output)

	if _, err := migrator.Run(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	output.Reset()

	reverted, err := migrator.Rollback(context.Background(), nil, Options{})
	if err != nil {
		t.Fatalf("Rollback: %v, and a declared irreversible migration must not stop the batch", err)
	}

	printed := output.String()
	if !strings.Contains(printed, "2026_01_01_000000_backfill_totals Skipped:") {
		t.Fatalf("the rollback never said it skipped the backfill:\n%s", printed)
	}
	if strings.Join(reverted, ",") != "2026_01_02_000000_add_index" {
		t.Fatalf("Rollback answered %v after printing:\n%s\nand the two have to say the same thing about the backfill", reverted, printed)
	}
}

// TestRollbackUnderPretendAnswersWhatARealRunWouldUndo pins a decision rather
// than an accident.
//
// Pretend runs nothing, so nothing is undone and every record survives. The
// names it answers with are still the ones a real run would undo, because that
// is the whole of the question pretending is asked -- an empty answer would say
// "this would roll nothing back", which is the opposite of true, and migrate
// already reports its pending migrations the same way under --pretend. The
// migration that will be left applied is left out here too: a real run would not
// undo it either.
func TestRollbackUnderPretendAnswersWhatARealRunWouldUndo(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups, downs []string
	Register(backfillMigration{oneWayMigration{name: "2026_01_01_000000_backfill_totals", ups: &ups}})
	Register(fakeMigration{name: "2026_01_02_000000_add_index", ups: &ups, downs: &downs})

	migrator, repository, _ := newTestMigrator()

	if _, err := migrator.Run(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reverted, err := migrator.Rollback(context.Background(), nil, Options{Pretend: true})
	if err != nil {
		t.Fatalf("Rollback --pretend: %v", err)
	}
	if strings.Join(reverted, ",") != "2026_01_02_000000_add_index" {
		t.Fatalf("Rollback --pretend answered %v, want what a real run would undo", reverted)
	}
	if len(downs) != 0 {
		t.Fatalf("Down ran for %v, and pretending runs nothing", downs)
	}
	if len(repository.records) != 2 {
		t.Fatalf("the repository holds %v, and pretending removes no record", repository.records)
	}
}

// TestRollbackDoesNotReportAMigrationWhoseCodeIsGoneAsReverted pins the case
// that was already right, so that a later change cannot make it wrong.
//
// A record whose migration is no longer registered is passed over rather than
// stopping the rollback, and it is passed over before anything is collected.
// Nothing undid that change, its row stays, and reporting it undone would be the
// same untruth as the one above for a different reason.
func TestRollbackDoesNotReportAMigrationWhoseCodeIsGoneAsReverted(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups, downs []string
	Register(fakeMigration{name: "2026_01_01_000000_deleted_later", ups: &ups, downs: &downs})
	Register(fakeMigration{name: "2026_01_02_000000_add_index", ups: &ups, downs: &downs})

	migrator, repository, _ := newTestMigrator()

	if _, err := migrator.Run(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The release that deleted the file: the row is still in the repository and
	// the code is no longer in the binary.
	flushRegistry()
	Register(fakeMigration{name: "2026_01_02_000000_add_index", ups: &ups, downs: &downs})

	reverted, err := migrator.Rollback(context.Background(), nil, Options{})
	if err != nil {
		t.Fatalf("Rollback: %v, and one deleted migration must not stop a rollback", err)
	}
	if strings.Join(reverted, ",") != "2026_01_02_000000_add_index" {
		t.Fatalf("Rollback answered %v, and nothing undid the migration whose code is gone", reverted)
	}
	if strings.Join(downs, ",") != "2026_01_02_000000_add_index" {
		t.Fatalf("the rollback ran Down for %v, want only the migration still registered", downs)
	}
	if len(repository.records) != 1 || repository.records[0].Migration != "2026_01_01_000000_deleted_later" {
		t.Fatalf("the repository holds %v, and the row of a migration nobody undid stays", repository.records)
	}
}

// TestRollbackUndoesNothingWhenOneMigrationInTheBatchCannotBeUndone is about
// the batch, not the migration.
//
// The refusal itself is older than this test: runDown has always answered an
// error for a migration declaring neither Down nor Irreversible. But it answered
// it on reaching that migration, and the rollback undoes newest first -- so
// everything applied after it was already undone and its records already
// deleted. The command stopped in the middle, which is the one state a migrator
// has no way back from: half a batch is not a version anybody deployed.
//
// The batch here is that shape. The reversible migration is the newer of the
// two, so a rollback that starts undoing reaches it first and only then finds
// the one it cannot undo.
func TestRollbackUndoesNothingWhenOneMigrationInTheBatchCannotBeUndone(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups, downs []string
	Register(oneWayMigration{name: "2026_01_01_000000_add_column", ups: &ups})
	Register(fakeMigration{name: "2026_01_02_000000_add_index", ups: &ups, downs: &downs})

	migrator, repository, _ := newTestMigrator()

	if _, err := migrator.Run(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	rolledBack, err := migrator.Rollback(context.Background(), nil, Options{})
	if err == nil {
		t.Fatal("Rollback reported success for a batch it could not undo whole")
	}
	if !strings.Contains(err.Error(), "2026_01_01_000000_add_column") {
		t.Fatalf("Rollback: %v, and the error has to name the migration that stopped it", err)
	}
	if !strings.Contains(err.Error(), "neither Down nor Irreversible") {
		t.Fatalf("Rollback: %v, and the error has to name both ways out", err)
	}

	// The whole of the point: the reversible migration was not undone, even
	// though the rollback would have reached it first.
	if len(downs) != 0 {
		t.Fatalf("Down ran for %v before the batch was refused, so the rollback stopped in the middle", downs)
	}
	if len(rolledBack) != 0 {
		t.Fatalf("Rollback reported undoing %v, and it undid nothing", rolledBack)
	}
	if len(repository.records) != 2 {
		t.Fatalf("the repository holds %v, and both migrations are still applied", repository.records)
	}
}

// TestResetUndoesNothingWhenOneMigrationCannotBeUndone is the same property on
// the other entry point. Reset and Rollback choose different records and undo
// them the same way, so the check belongs where they meet rather than in each.
func TestResetUndoesNothingWhenOneMigrationCannotBeUndone(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups, downs []string
	Register(oneWayMigration{name: "2026_01_01_000000_add_column", ups: &ups})
	Register(fakeMigration{name: "2026_01_02_000000_add_index", ups: &ups, downs: &downs})

	migrator, repository, _ := newTestMigrator()

	if _, err := migrator.Run(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := migrator.Reset(context.Background(), nil, false); err == nil {
		t.Fatal("Reset reported success for a batch it could not undo whole")
	}
	if len(downs) != 0 {
		t.Fatalf("Down ran for %v before Reset was refused, so it stopped in the middle", downs)
	}
	if len(repository.records) != 2 {
		t.Fatalf("the repository holds %v, and both migrations are still applied", repository.records)
	}
}

// TestResetDoesNotReportAnIrreversibleMigrationAsReverted is the same property
// on the other entry point. Reset and Rollback choose different records and undo
// them through the same loop, so what one leaves out of its answer the other
// leaves out too.
func TestResetDoesNotReportAnIrreversibleMigrationAsReverted(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups, downs []string
	Register(backfillMigration{oneWayMigration{name: "2026_01_01_000000_backfill_totals", ups: &ups}})
	Register(fakeMigration{name: "2026_01_02_000000_add_index", ups: &ups, downs: &downs})

	migrator, repository, _ := newTestMigrator()

	if _, err := migrator.Run(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reverted, err := migrator.Reset(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("Reset: %v, and a declared irreversible migration must not stop it", err)
	}
	if strings.Join(reverted, ",") != "2026_01_02_000000_add_index" {
		t.Fatalf("Reset answered %v, and a reset is not a guarantee of an empty schema", reverted)
	}
	if len(repository.records) != 1 || repository.records[0].Migration != "2026_01_01_000000_backfill_totals" {
		t.Fatalf("the repository holds %v, and the backfill is still applied", repository.records)
	}
}

// TestRollbackSaysUpFrontWhichMigrationsItWillLeaveApplied is the other half of
// reading the batch first. A migration declaring Irreversible does not stop the
// rollback -- it is skipped and left applied -- so the preflight must not refuse
// it. What it does instead is name it before the first Down runs, because "this
// batch will not come back whole" is worth knowing before the command starts
// rather than after.
func TestRollbackSaysUpFrontWhichMigrationsItWillLeaveApplied(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups, downs []string
	Register(backfillMigration{oneWayMigration{name: "2026_01_01_000000_backfill_totals", ups: &ups}})
	Register(fakeMigration{name: "2026_01_02_000000_add_index", ups: &ups, downs: &downs})

	migrator, _, _ := newTestMigrator()
	output := &strings.Builder{}
	migrator.SetOutput(output)

	if _, err := migrator.Run(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	output.Reset()

	if _, err := migrator.Rollback(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Rollback: %v, and a declared irreversible migration must not stop the batch", err)
	}

	printed := output.String()
	notice := strings.Index(printed, "2026_01_01_000000_backfill_totals will be left applied")
	if notice < 0 {
		t.Fatalf("the rollback never said the backfill would be left applied:\n%s", printed)
	}
	started := strings.Index(printed, "Rolling back migrations.")
	if started < 0 {
		t.Fatalf("the rollback never announced itself:\n%s", printed)
	}
	if notice > started {
		t.Fatalf("the notice came after the rollback began, and it exists to be read before:\n%s", printed)
	}

	// And the reversible one in the same batch was still undone.
	if strings.Join(downs, ",") != "2026_01_02_000000_add_index" {
		t.Fatalf("the rollback ran Down for %v, want only the reversible migration", downs)
	}
}

func TestRollbackRefusesAMigrationClaimingBothDirections(t *testing.T) {
	t.Cleanup(flushRegistry)
	flushRegistry()

	var ups, downs []string
	Register(contradictoryMigration{fakeMigration{name: "2026_01_01_000000_both", ups: &ups, downs: &downs}})

	migrator, repository, _ := newTestMigrator()

	if _, err := migrator.Run(context.Background(), nil, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	_, err := migrator.Rollback(context.Background(), nil, Options{})
	if err == nil {
		t.Fatal("Rollback picked one of two opposite claims instead of refusing")
	}
	if len(downs) != 0 {
		t.Fatalf("Down ran for %v, and a migration that also says it is irreversible must not be guessed at", downs)
	}
	if len(repository.records) != 1 {
		t.Fatalf("the repository holds %v after a refused rollback", repository.records)
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
