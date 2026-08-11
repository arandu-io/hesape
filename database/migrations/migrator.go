package migrations

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/arandu-io/hesape/database/events"
)

// Dispatcher answers Illuminate\Contracts\Events\Dispatcher, narrowed to the
// one method the Migrator calls on it.
type Dispatcher interface {
	// Dispatch fires an event.
	Dispatch(event any)
}

// TransactionalConnection is a Connection that can wrap a migration in a
// transaction.
//
// The PHP tests two things before wrapping: the schema grammar's
// supportsSchemaTransactions, and the migration's own $withinTransaction. The
// first belongs to the connection and is asked for here; the second is on the
// Migration. A connection that does not satisfy this interface runs its
// migrations unwrapped, which is what the PHP does for MySQL -- it has no
// transactional DDL, so a failed migration there leaves half a schema whatever
// anybody wants.
type TransactionalConnection interface {
	Connection

	// SupportsSchemaTransactions answers the schema grammar method of the same
	// name.
	SupportsSchemaTransactions() bool

	// Transaction answers Connection::transaction.
	Transaction(ctx context.Context, callback func() error) error
}

// Options answers the $options array the Migrator's methods take.
//
// PHP reads 'pretend', 'step' and 'batch' out of one untyped array, where
// 'step' means two different things: a bool on the way up (one batch per
// migration) and an int on the way down (how many to undo). Two fields is the
// same information with the ambiguity removed, and the ambiguity is worth
// removing -- `--step` on migrate and `--step=3` on rollback are not the same
// flag.
type Options struct {
	// Pretend answers $options['pretend']: print the statements, run none.
	Pretend bool

	// Step answers $options['step'] on the way up: give every migration its
	// own batch, so each can be rolled back on its own.
	Step bool

	// Steps answers $options['step'] on the way down: how many migrations to
	// roll back.
	Steps int

	// Batch answers $options['batch']: roll back one named batch.
	Batch int
}

// connectionResolverCallback answers Migrator::$connectionResolverCallback, and
// is static there for the same reason it is package-level here: Laravel sets it
// once, from a service provider, for the whole process.
var (
	connectionResolverMu       sync.RWMutex
	connectionResolverCallback func(resolver Resolver, name string) (Connection, error)

	withoutMigrationsMu sync.RWMutex
	withoutMigrations   []string
)

// Migrator answers Illuminate\Database\Migrations\Migrator: the thing that runs
// migrations up and down and keeps the repository in step with the schema.
//
// # It does not run at boot, and this is where that is enforced
//
// RULE 16: `aru migrate` is a step of the deployment pipeline, never a call in
// the start-up path of the process. With N replicas rolling, calling Run from
// main means N migrators racing each other over the same table, and the one
// that loses reports a duplicate key on a table it was creating. There is no
// Migrate-on-boot helper here to make that easy, and there will not be one.
//
// The other half of the same rule is the migration's own: every migration is
// compatible with the binary that is still running while the rollout finishes.
// A new column is nullable or has a default; removing one takes two releases,
// the first stopping the writes and the second dropping the column.
type Migrator struct {
	// events is Migrator::$events.
	events Dispatcher

	// repository is Migrator::$repository.
	repository MigrationRepositoryInterface

	// resolver is Migrator::$resolver.
	resolver Resolver

	// connection is Migrator::$connection: the default connection name.
	connection string

	// paths is Migrator::$paths.
	paths []string

	// output is Migrator::$output, an io.Writer rather than a Symfony
	// OutputInterface. Nil writes nothing, which is what a null output does
	// there.
	output io.Writer
}

// NewMigrator answers Migrator::__construct.
//
// The PHP takes a Filesystem, and there is none here: a migration is code, and
// the registry replaced the glob. See Register for the whole of that decision.
func NewMigrator(repository MigrationRepositoryInterface, resolver Resolver, dispatcher Dispatcher) *Migrator {
	return &Migrator{repository: repository, resolver: resolver, events: dispatcher}
}

// Run answers Migrator::run: apply everything that has not been applied, and
// answer the names of what it applied.
func (m *Migrator) Run(ctx context.Context, paths []string, options Options) ([]string, error) {
	files := m.GetMigrationFiles(paths)

	ran, err := m.repository.GetRan(ctx)
	if err != nil {
		return nil, err
	}

	pending := m.pendingMigrations(files, ran)

	if err := m.RunPending(ctx, pending, options); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(pending))
	for _, migration := range pending {
		names = append(names, migration.GetName())
	}
	return names, nil
}

// pendingMigrations answers the protected Migrator::pendingMigrations.
func (m *Migrator) pendingMigrations(files map[string]Migration, ran []string) []Migration {
	done := make(map[string]bool, len(ran))
	for _, name := range ran {
		done[name] = true
	}
	for _, name := range m.migrationsToSkip() {
		done[name] = true
	}

	names := make([]string, 0, len(files))
	for name := range files {
		if !done[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	out := make([]Migration, 0, len(names))
	for _, name := range names {
		out = append(out, files[name])
	}
	return out
}

// migrationsToSkip answers the protected Migrator::migrationsToSkip.
func (m *Migrator) migrationsToSkip() []string {
	withoutMigrationsMu.RLock()
	defer withoutMigrationsMu.RUnlock()

	out := make([]string, 0, len(withoutMigrations))
	for _, name := range withoutMigrations {
		out = append(out, m.GetMigrationName(name))
	}
	return out
}

// RunPending answers Migrator::runPending: apply the given migrations, in the
// order they arrive.
//
// It stops at the first failure. Applying later migrations over a schema that a
// failed one left half-changed turns one clear error into a database nobody can
// get back.
func (m *Migrator) RunPending(ctx context.Context, migrations []Migration, options Options) error {
	if len(migrations) == 0 {
		m.FireMigrationEvent(events.NewNoPendingMigrations("up"))
		m.write("Nothing to migrate")
		return nil
	}

	batch, err := m.repository.GetNextBatchNumber(ctx)
	if err != nil {
		return err
	}

	m.FireMigrationEvent(events.NewMigrationsStarted("up", optionsMap(options)))
	m.write("Running migrations.")

	for _, migration := range migrations {
		if err := m.runUp(ctx, migration, batch, options.Pretend); err != nil {
			return err
		}
		if options.Step {
			batch++
		}
	}

	m.FireMigrationEvent(events.NewMigrationsEnded("up", optionsMap(options)))
	m.write("")

	return nil
}

// runUp answers the protected Migrator::runUp.
func (m *Migrator) runUp(ctx context.Context, migration Migration, batch int, pretend bool) error {
	name := migration.GetName()

	if pretend {
		return m.pretendToRun(ctx, migration, "up")
	}

	if !migration.ShouldRun() {
		m.FireMigrationEvent(events.NewMigrationSkipped(name))
		m.write(fmt.Sprintf("%s %s", name, Skipped))
		return nil
	}

	if err := m.runMigration(ctx, migration, "up"); err != nil {
		m.write(fmt.Sprintf("%s %s", name, Failure))
		return fmt.Errorf("migration %s failed: %w", name, err)
	}

	// Recorded only after it succeeded. A name in the table for a migration
	// that did not finish is the one state a migrator cannot recover from.
	if err := m.repository.Log(ctx, name, batch); err != nil {
		return err
	}

	m.write(fmt.Sprintf("%s %s", name, Success))
	return nil
}

// Rollback answers Migrator::rollback: undo the last batch, or the batch or
// step count the options name.
func (m *Migrator) Rollback(ctx context.Context, paths []string, options Options) ([]string, error) {
	records, err := m.getMigrationsForRollback(ctx, options)
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		m.FireMigrationEvent(events.NewNoPendingMigrations("down"))
		m.write("Nothing to rollback.")
		return []string{}, nil
	}

	rolledBack, err := m.rollbackMigrations(ctx, records, paths, options)
	m.write("")
	return rolledBack, err
}

// getMigrationsForRollback answers the protected
// Migrator::getMigrationsForRollback.
func (m *Migrator) getMigrationsForRollback(ctx context.Context, options Options) ([]MigrationRecord, error) {
	if options.Steps > 0 {
		return m.repository.GetMigrations(ctx, options.Steps)
	}
	if options.Batch > 0 {
		return m.repository.GetMigrationsByBatch(ctx, options.Batch)
	}
	return m.repository.GetLast(ctx)
}

// rollbackMigrations answers the protected Migrator::rollbackMigrations.
//
// A recorded migration whose code is no longer registered is reported and
// skipped, exactly as the PHP prints "Migration not found" and continues: the
// alternative is a rollback that refuses to start because of one file somebody
// deleted six releases ago.
func (m *Migrator) rollbackMigrations(ctx context.Context, records []MigrationRecord, paths []string, options Options) ([]string, error) {
	var rolledBack []string

	files := m.GetMigrationFiles(paths)

	m.FireMigrationEvent(events.NewMigrationsStarted("down", optionsMap(options)))
	m.write("Rolling back migrations.")

	for _, record := range records {
		migration, known := files[record.Migration]
		if !known {
			m.write(fmt.Sprintf("%s Migration not found", record.Migration))
			continue
		}

		rolledBack = append(rolledBack, record.Migration)

		if err := m.runDown(ctx, migration, record, options.Pretend); err != nil {
			return rolledBack, err
		}
	}

	m.FireMigrationEvent(events.NewMigrationsEnded("down", optionsMap(options)))

	return rolledBack, nil
}

// Reset answers Migrator::reset: roll every applied migration back, newest
// first.
func (m *Migrator) Reset(ctx context.Context, paths []string, pretend bool) ([]string, error) {
	ran, err := m.repository.GetRan(ctx)
	if err != nil {
		return nil, err
	}
	if len(ran) == 0 {
		m.write("Nothing to rollback.")
		return []string{}, nil
	}

	records := make([]MigrationRecord, 0, len(ran))
	for _, name := range ran {
		records = append(records, MigrationRecord{Migration: name})
	}
	// GetRan answers oldest first, and a reset undoes them the other way.
	sortRecordsByName(records, true)

	rolledBack, err := m.rollbackMigrations(ctx, records, paths, Options{Pretend: pretend})
	m.write("")
	return rolledBack, err
}

// runDown answers the protected Migrator::runDown.
func (m *Migrator) runDown(ctx context.Context, migration Migration, record MigrationRecord, pretend bool) error {
	name := migration.GetName()

	if pretend {
		return m.pretendToRun(ctx, migration, "down")
	}

	if err := m.runMigration(ctx, migration, "down"); err != nil {
		m.write(fmt.Sprintf("%s %s", name, Failure))
		return fmt.Errorf("rollback of %s failed: %w", name, err)
	}

	if err := m.repository.Delete(ctx, record); err != nil {
		return err
	}

	m.write(fmt.Sprintf("%s %s", name, Success))
	return nil
}

// runMigration answers the protected Migrator::runMigration: run one direction
// of one migration, inside a transaction when the engine and the migration both
// allow it.
func (m *Migrator) runMigration(ctx context.Context, migration Migration, method string) error {
	conn, err := m.ResolveConnection(migration.GetConnection())
	if err != nil {
		return err
	}

	callback := func() error {
		reversible, isReversible := migration.(ReversibleMigration)
		if method == "down" && !isReversible {
			// The PHP's method_exists check: a migration with no down is not
			// reversed, and that is not an error there either.
			return nil
		}

		m.FireMigrationEvent(events.NewMigrationStarted(migration, method))

		var err error
		if method == "down" {
			err = reversible.Down(ctx, conn)
		} else {
			err = migration.Up(ctx, conn)
		}
		if err != nil {
			return err
		}

		m.FireMigrationEvent(events.NewMigrationEnded(migration, method))
		return nil
	}

	transactional, ok := conn.(TransactionalConnection)
	if ok && transactional.SupportsSchemaTransactions() && migration.WithinTransaction() {
		return transactional.Transaction(ctx, callback)
	}
	return callback()
}

// pretendToRun answers the protected Migrator::pretendToRun.
//
// The PHP collects the statements by running the migration against a connection
// in "dry run" mode and reading its query log. That needs a Connection, so the
// same trick is used here: a connection that implements PretendingConnection
// answers the statements without executing them, and one that does not says so
// rather than pretending to pretend.
func (m *Migrator) pretendToRun(ctx context.Context, migration Migration, method string) error {
	name := migration.GetName()
	m.write(name)

	conn, err := m.ResolveConnection(migration.GetConnection())
	if err != nil {
		return err
	}

	pretender, ok := conn.(PretendingConnection)
	if !ok {
		m.write("  this connection cannot pretend, so nothing was printed and nothing ran")
		return nil
	}

	queries, err := pretender.Pretend(ctx, func() error {
		if method == "down" {
			if reversible, isReversible := migration.(ReversibleMigration); isReversible {
				return reversible.Down(ctx, conn)
			}
			return nil
		}
		return migration.Up(ctx, conn)
	})
	if err != nil {
		return err
	}

	for _, query := range queries {
		m.write("  " + query)
	}
	return nil
}

// PretendingConnection is a Connection that can run a callback without letting
// any of its statements reach the server.
//
// It answers Connection::pretend, which returns the query log rather than the
// callback's result.
type PretendingConnection interface {
	Connection

	// Pretend answers Connection::pretend: the statements the callback would
	// have run.
	Pretend(ctx context.Context, callback func() error) ([]string, error)
}

// Resolve answers Migrator::resolve: the migration registered under a name.
//
// The PHP builds a class name out of the file name and news it up. Here the
// registry already holds the instance, because a Go migration is registered
// rather than discovered -- so this is a lookup, and an unknown name is an
// error rather than a fatal on `new`.
func (m *Migrator) Resolve(name string) (Migration, error) {
	for _, migration := range Registered(m.paths...) {
		if migration.GetName() == name {
			return migration, nil
		}
	}
	return nil, fmt.Errorf("no migration is registered under %q", name)
}

// GetMigrationFiles answers Migrator::getMigrationFiles: every migration of the
// given paths, keyed by name.
//
// The PHP globs `*_*.php` and keys by the file name minus the extension. There
// is no glob here -- see Register -- so this reads the registry and keys by
// GetName, which is the same string the PHP's key is.
func (m *Migrator) GetMigrationFiles(paths []string) map[string]Migration {
	groups := append(append([]string(nil), m.paths...), paths...)

	out := map[string]Migration{}
	for _, migration := range Registered(groups...) {
		out[migration.GetName()] = migration
	}
	return out
}

// RequireFiles has no counterpart, and its absence is the point.
//
// It exists in PHP to require_once each migration file, because a file on disk
// is not a class until something reads it. Go has no such step: the import in
// main.go put every registered migration in the binary before it started.

// GetMigrationName answers Migrator::getMigrationName: the name of a migration,
// given either the name itself or a path that ends in it.
//
// It still takes a path-shaped string because `aru migrate --without=` and the
// squashed-schema paths hand it one, and because a person copying a file name
// out of a log should get the right answer.
func (m *Migrator) GetMigrationName(path string) string {
	name := path
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".go")
	return strings.TrimSuffix(name, ".php")
}

// Path answers Migrator::path: add a group the Migrator should look in.
func (m *Migrator) Path(path string) {
	for _, existing := range m.paths {
		if existing == path {
			return
		}
	}
	m.paths = append(m.paths, path)
}

// Paths answers Migrator::paths.
func (m *Migrator) Paths() []string { return m.paths }

// WithoutMigrations answers Migrator::withoutMigrations: names to leave pending
// however many times migrate runs.
//
// It is static in PHP and package-level here for the same reason: the test
// suite sets it once for a process.
func WithoutMigrations(names []string) {
	withoutMigrationsMu.Lock()
	defer withoutMigrationsMu.Unlock()
	withoutMigrations = names
}

// GetConnection answers Migrator::getConnection: the default connection name.
func (m *Migrator) GetConnection() string { return m.connection }

// UsingConnection answers Migrator::usingConnection: run the callback with a
// different default connection, and put the old one back afterwards.
func (m *Migrator) UsingConnection(name string, callback func() error) error {
	previous := m.resolver.GetDefaultConnection()

	m.SetConnection(name)
	defer m.SetConnection(previous)

	return callback()
}

// SetConnection answers Migrator::setConnection.
func (m *Migrator) SetConnection(name string) {
	if name != "" {
		m.resolver.SetDefaultConnection(name)
	}
	m.repository.SetSource(name)
	m.connection = name
}

// ResolveConnection answers Migrator::resolveConnection.
func (m *Migrator) ResolveConnection(connection string) (Connection, error) {
	connectionResolverMu.RLock()
	callback := connectionResolverCallback
	connectionResolverMu.RUnlock()

	name := connection
	if name == "" {
		name = m.connection
	}

	if callback != nil {
		return callback(m.resolver, name)
	}
	return m.resolver.Connection(name)
}

// ResolveConnectionsUsing answers Migrator::resolveConnectionsUsing.
func ResolveConnectionsUsing(callback func(resolver Resolver, name string) (Connection, error)) {
	connectionResolverMu.Lock()
	defer connectionResolverMu.Unlock()
	connectionResolverCallback = callback
}

// GetRepository answers Migrator::getRepository.
func (m *Migrator) GetRepository() MigrationRepositoryInterface { return m.repository }

// RepositoryExists answers Migrator::repositoryExists.
func (m *Migrator) RepositoryExists(ctx context.Context) bool {
	return m.repository.RepositoryExists(ctx)
}

// HasRunAnyMigrations answers Migrator::hasRunAnyMigrations.
func (m *Migrator) HasRunAnyMigrations(ctx context.Context) bool {
	if !m.RepositoryExists(ctx) {
		return false
	}
	ran, err := m.repository.GetRan(ctx)
	return err == nil && len(ran) > 0
}

// DeleteRepository answers Migrator::deleteRepository.
func (m *Migrator) DeleteRepository(ctx context.Context) error {
	return m.repository.DeleteRepository(ctx)
}

// SetOutput answers Migrator::setOutput.
//
// The PHP takes a Symfony OutputInterface and renders console components
// through it. An io.Writer is what that is once the components are gone, and
// they are gone because a library that draws a table is a library a test cannot
// read.
func (m *Migrator) SetOutput(output io.Writer) *Migrator {
	m.output = output
	return m
}

// FireMigrationEvent answers Migrator::fireMigrationEvent.
func (m *Migrator) FireMigrationEvent(event any) {
	if m.events != nil {
		m.events.Dispatch(event)
	}
}

// write answers the protected Migrator::write.
func (m *Migrator) write(line string) {
	if m.output == nil {
		return
	}
	_, _ = io.WriteString(m.output, line+"\n")
}

// optionsMap shapes Options back into the array the events carry, so a listener
// written against Laravel reads the keys it expects.
func optionsMap(options Options) map[string]any {
	return map[string]any{
		"pretend": options.Pretend,
		"step":    options.Step,
		"steps":   options.Steps,
		"batch":   options.Batch,
	}
}
