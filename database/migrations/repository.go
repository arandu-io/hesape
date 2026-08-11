package migrations

import (
	"context"
	"fmt"
	"sort"
)

// Resolver answers Illuminate\Database\ConnectionResolverInterface, narrowed to
// what the repository and the Migrator ask of it.
//
// It is declared here rather than imported for the reason every interface in
// this component is: the database package resolves migrations, so it imports
// this one, and naming its concrete resolver here would close the cycle.
type Resolver interface {
	// Connection answers ConnectionResolverInterface::connection. The PHP
	// throws for an unknown name; this returns the error.
	Connection(name string) (Connection, error)

	// GetDefaultConnection answers
	// ConnectionResolverInterface::getDefaultConnection.
	GetDefaultConnection() string

	// SetDefaultConnection answers
	// ConnectionResolverInterface::setDefaultConnection.
	SetDefaultConnection(name string)
}

// MigrationRecord is one row of the repository table.
//
// PHP has no name for it -- the interface's docblock calls it
// `array{id: int, migration: string, batch: int}` -- so this is the shape it
// describes, given the name it never got.
type MigrationRecord struct {
	// ID is the row's ordinal.
	ID int

	// Migration is the migration's name, the same string GetName answers.
	Migration string

	// Batch is the run it belonged to. One `aru migrate` is one batch, which
	// is what makes a rollback undo a deploy rather than a single file.
	Batch int
}

// MigrationRepositoryInterface answers
// Illuminate\Database\Migrations\MigrationRepositoryInterface.
//
// Every method the PHP declares is here, with an error added wherever it talks
// to the database -- which is all of them. The PHP lets a PDOException travel
// out of these unannounced; a Go caller that cannot tell "no migrations have
// run" from "the connection is gone" writes a migrate command that reports
// success on a dead database.
type MigrationRepositoryInterface interface {
	// GetRan answers getRan: the names of every applied migration, ordered by
	// batch and then by name.
	GetRan(ctx context.Context) ([]string, error)

	// GetMigrations answers getMigrations: the last N applied, most recent
	// first.
	GetMigrations(ctx context.Context, steps int) ([]MigrationRecord, error)

	// GetMigrationsByBatch answers getMigrationsByBatch.
	GetMigrationsByBatch(ctx context.Context, batch int) ([]MigrationRecord, error)

	// GetLast answers getLast: the whole of the most recent batch, in reverse
	// order, which is the order a rollback undoes them in.
	GetLast(ctx context.Context) ([]MigrationRecord, error)

	// GetMigrationBatches answers getMigrationBatches: name to batch, for the
	// status table.
	GetMigrationBatches(ctx context.Context) (map[string]int, error)

	// Log answers log: record that a migration ran.
	Log(ctx context.Context, file string, batch int) error

	// Delete answers delete: forget that one did.
	Delete(ctx context.Context, migration MigrationRecord) error

	// GetNextBatchNumber answers getNextBatchNumber.
	GetNextBatchNumber(ctx context.Context) (int, error)

	// CreateRepository answers createRepository: create the table.
	CreateRepository(ctx context.Context) error

	// RepositoryExists answers repositoryExists.
	RepositoryExists(ctx context.Context) bool

	// DeleteRepository answers deleteRepository: drop the table.
	DeleteRepository(ctx context.Context) error

	// SetSource answers setSource: the connection to read and write the table
	// on.
	SetSource(name string)
}

// DatabaseMigrationRepository answers
// Illuminate\Database\Migrations\DatabaseMigrationRepository: the table that
// remembers which migrations have run.
//
// The PHP builds its queries through the fluent builder and its schema builder;
// these are written out as SQL, because that is what this framework does
// everywhere else and because the four statements involved are the same on all
// three engines.
type DatabaseMigrationRepository struct {
	// resolver is DatabaseMigrationRepository::$resolver.
	resolver Resolver

	// table is DatabaseMigrationRepository::$table.
	table string

	// connection is DatabaseMigrationRepository::$connection: the name
	// SetSource put there.
	connection string
}

// DefaultTable is the table name Laravel uses, and the one to pass to
// NewDatabaseMigrationRepository unless a project has a reason not to.
//
// The database package's own MigrationsTable is arandu_migrations, and the two
// are not the same table: that one tracks database.Migration, the SQL-pair
// shape this framework generates, and this one tracks a migrations.Migration.
// A project uses one or the other, never both, which is why neither defaults to
// the other's name.
const DefaultTable = "migrations"

// NewDatabaseMigrationRepository answers
// DatabaseMigrationRepository::__construct.
func NewDatabaseMigrationRepository(resolver Resolver, table string) *DatabaseMigrationRepository {
	return &DatabaseMigrationRepository{resolver: resolver, table: table}
}

// GetRan answers DatabaseMigrationRepository::getRan.
func (r *DatabaseMigrationRepository) GetRan(ctx context.Context) ([]string, error) {
	records, err := r.query(ctx, `SELECT id, migration, batch FROM `+r.table+` ORDER BY batch ASC, migration ASC`)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(records))
	for _, record := range records {
		out = append(out, record.Migration)
	}
	return out, nil
}

// GetMigrations answers DatabaseMigrationRepository::getMigrations: the last
// `steps` applied, newest first.
//
// The PHP's `where('batch', '>=', '1')` is here too, and it is not redundant:
// a batch of zero is what `migrate --pretend` and hand-written rows leave
// behind, and neither should be rolled back.
func (r *DatabaseMigrationRepository) GetMigrations(ctx context.Context, steps int) ([]MigrationRecord, error) {
	records, err := r.query(ctx,
		`SELECT id, migration, batch FROM `+r.table+
			` WHERE batch >= 1 ORDER BY batch DESC, migration DESC LIMIT ?`, steps)
	if err != nil {
		return nil, err
	}
	return records, nil
}

// GetMigrationsByBatch answers
// DatabaseMigrationRepository::getMigrationsByBatch.
func (r *DatabaseMigrationRepository) GetMigrationsByBatch(ctx context.Context, batch int) ([]MigrationRecord, error) {
	return r.query(ctx,
		`SELECT id, migration, batch FROM `+r.table+` WHERE batch = ? ORDER BY migration DESC`, batch)
}

// GetLast answers DatabaseMigrationRepository::getLast: the most recent batch,
// in the order a rollback wants it.
func (r *DatabaseMigrationRepository) GetLast(ctx context.Context) ([]MigrationRecord, error) {
	last, err := r.GetLastBatchNumber(ctx)
	if err != nil {
		return nil, err
	}
	return r.query(ctx,
		`SELECT id, migration, batch FROM `+r.table+` WHERE batch = ? ORDER BY migration DESC`, last)
}

// GetMigrationBatches answers
// DatabaseMigrationRepository::getMigrationBatches: the name-to-batch map
// `migrate:status` prints.
func (r *DatabaseMigrationRepository) GetMigrationBatches(ctx context.Context) (map[string]int, error) {
	records, err := r.query(ctx, `SELECT id, migration, batch FROM `+r.table+` ORDER BY batch ASC, migration ASC`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(records))
	for _, record := range records {
		out[record.Migration] = record.Batch
	}
	return out, nil
}

// Log answers DatabaseMigrationRepository::log.
//
// The id is computed rather than left to the engine. The PHP's table declares
// `$table->increments('id')`, and the three engines spell auto-increment three
// different ways -- SERIAL, AUTO_INCREMENT, AUTOINCREMENT -- so a portable
// CREATE TABLE cannot have one. Nothing reads the id for anything but order,
// and migrations run one at a time in a pipeline step (RULE 16), so max + 1 is
// the whole of it.
func (r *DatabaseMigrationRepository) Log(ctx context.Context, file string, batch int) error {
	conn, err := r.GetConnection()
	if err != nil {
		return err
	}

	next, err := r.nextID(ctx)
	if err != nil {
		return err
	}

	_, err = conn.Statement(ctx,
		`INSERT INTO `+r.table+` (id, migration, batch) VALUES (?, ?, ?)`,
		[]any{next, file, batch})
	if err != nil {
		return fmt.Errorf("recording migration %s: %w", file, err)
	}
	return nil
}

// Delete answers DatabaseMigrationRepository::delete.
func (r *DatabaseMigrationRepository) Delete(ctx context.Context, migration MigrationRecord) error {
	conn, err := r.GetConnection()
	if err != nil {
		return err
	}
	_, err = conn.Statement(ctx, `DELETE FROM `+r.table+` WHERE migration = ?`, []any{migration.Migration})
	if err != nil {
		return fmt.Errorf("clearing the record of %s: %w", migration.Migration, err)
	}
	return nil
}

// GetNextBatchNumber answers
// DatabaseMigrationRepository::getNextBatchNumber.
func (r *DatabaseMigrationRepository) GetNextBatchNumber(ctx context.Context) (int, error) {
	last, err := r.GetLastBatchNumber(ctx)
	if err != nil {
		return 0, err
	}
	return last + 1, nil
}

// GetLastBatchNumber answers
// DatabaseMigrationRepository::getLastBatchNumber.
//
// The PHP asks the builder for max('batch'), which answers null on an empty
// table and becomes zero on the way out. Reading the rows is the same answer on
// a table that never holds more than a few hundred of them, and it avoids a
// MAX() that returns NULL and a driver that scans NULL into an int.
func (r *DatabaseMigrationRepository) GetLastBatchNumber(ctx context.Context) (int, error) {
	records, err := r.query(ctx, `SELECT id, migration, batch FROM `+r.table)
	if err != nil {
		return 0, err
	}
	last := 0
	for _, record := range records {
		if record.Batch > last {
			last = record.Batch
		}
	}
	return last, nil
}

// CreateRepository answers DatabaseMigrationRepository::createRepository.
//
// The column types are the portable spellings: INTEGER and VARCHAR(255).
// VARCHAR rather than TEXT because migration is what every query here filters
// on and MySQL refuses a TEXT column in a key without a prefix length -- the
// mistake that once stopped `aru migrate` on its own first statement.
func (r *DatabaseMigrationRepository) CreateRepository(ctx context.Context) error {
	conn, err := r.GetConnection()
	if err != nil {
		return err
	}
	_, err = conn.Statement(ctx, `CREATE TABLE IF NOT EXISTS `+r.table+` (
	id        INTEGER NOT NULL,
	migration VARCHAR(255) NOT NULL,
	batch     INTEGER NOT NULL,
	PRIMARY KEY (migration)
)`, nil)
	if err != nil {
		return fmt.Errorf("creating %s: %w", r.table, err)
	}
	return nil
}

// RepositoryExists answers DatabaseMigrationRepository::repositoryExists.
//
// The PHP asks its schema builder, which asks the engine's catalogue with a
// query written three different ways. Selecting from the table and reading the
// answer is the same question asked in one way that works everywhere: a table
// that is not there cannot be selected from.
func (r *DatabaseMigrationRepository) RepositoryExists(ctx context.Context) bool {
	conn, err := r.GetConnection()
	if err != nil {
		return false
	}
	_, err = conn.Select(ctx, `SELECT migration FROM `+r.table+` WHERE 1 = 0`, nil)
	return err == nil
}

// DeleteRepository answers DatabaseMigrationRepository::deleteRepository.
func (r *DatabaseMigrationRepository) DeleteRepository(ctx context.Context) error {
	conn, err := r.GetConnection()
	if err != nil {
		return err
	}
	_, err = conn.Statement(ctx, `DROP TABLE `+r.table, nil)
	return err
}

// GetConnectionResolver answers
// DatabaseMigrationRepository::getConnectionResolver.
func (r *DatabaseMigrationRepository) GetConnectionResolver() Resolver { return r.resolver }

// GetConnection answers DatabaseMigrationRepository::getConnection.
func (r *DatabaseMigrationRepository) GetConnection() (Connection, error) {
	if r.resolver == nil {
		return nil, fmt.Errorf("the migration repository has no connection resolver")
	}
	return r.resolver.Connection(r.connection)
}

// SetSource answers DatabaseMigrationRepository::setSource.
func (r *DatabaseMigrationRepository) SetSource(name string) { r.connection = name }

// GetTable is the $table the repository was built with. The PHP reads the
// property directly, which Go cannot do from another package.
func (r *DatabaseMigrationRepository) GetTable() string { return r.table }

// nextID is the ordinal Log writes. See there for why it is computed.
func (r *DatabaseMigrationRepository) nextID(ctx context.Context) (int, error) {
	records, err := r.query(ctx, `SELECT id, migration, batch FROM `+r.table)
	if err != nil {
		return 0, err
	}
	next := 0
	for _, record := range records {
		if record.ID > next {
			next = record.ID
		}
	}
	return next + 1, nil
}

// query runs one of the reads above and shapes the rows.
func (r *DatabaseMigrationRepository) query(ctx context.Context, sql string, bindings ...any) ([]MigrationRecord, error) {
	conn, err := r.GetConnection()
	if err != nil {
		return nil, err
	}

	rows, err := conn.Select(ctx, sql, bindings)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", r.table, err)
	}

	out := make([]MigrationRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, MigrationRecord{
			ID:        asInt(row["id"]),
			Migration: asString(row["migration"]),
			Batch:     asInt(row["batch"]),
		})
	}
	return out, nil
}

// asInt reads an integer column whichever way the driver handed it over.
//
// database/sql gives an INTEGER back as int64 from one driver, int from
// another, and []byte from MySQL when the column went through a text protocol.
// A type switch here is what keeps that from being three bugs.
func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	case []byte:
		return parseInt(string(n))
	case string:
		return parseInt(n)
	default:
		return 0
	}
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func asString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// sortRecordsByName orders records the way the rollback paths want them.
func sortRecordsByName(records []MigrationRecord, descending bool) {
	sort.SliceStable(records, func(i, j int) bool {
		if descending {
			return records[i].Migration > records[j].Migration
		}
		return records[i].Migration < records[j].Migration
	})
}
