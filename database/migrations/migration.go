package migrations

import "context"

// Connection is what a migration runs against.
//
// It answers Illuminate\Database\Connection, narrowed to the two calls a schema
// change makes. The interface is declared here rather than imported because the
// database package resolves migrations, and naming the concrete type would
// close the cycle.
//
// There is no auth.Grant on it, and that is a decision rather than an
// oversight. RULE 17 governs the path to application rows: every List, Find,
// Get, Paginate, report and export goes through a Policy and filters by
// auth.Tenant(g). A migration is not on that path -- it is DDL, run by `aru
// migrate` as a pipeline step, in a process with no request, no subject and
// therefore nothing a Grant could be built from. database.Migrate next door has
// always taken none for the same reason, and giving one to a migration would
// mean inventing a fake subject, which is worse than not having one.
type Connection interface {
	// GetName answers Connection::getName.
	GetName() string

	// Statement answers Connection::statement: run a statement that returns
	// neither rows nor a count, which is every DDL statement there is.
	Statement(ctx context.Context, query string, bindings []any) (bool, error)

	// Select answers Connection::select, for the migration that has to read
	// before it writes -- a backfill, a check that a column is empty before
	// dropping it.
	Select(ctx context.Context, query string, bindings []any) ([]map[string]any, error)
}

// Migration answers Illuminate\Database\Migrations\Migration.
//
// The PHP is an abstract class with two halves: the up() and down() a subclass
// writes, and the getConnection(), shouldRun() and $withinTransaction it
// inherits. Go has no abstract class, so the whole surface is this interface,
// and BaseMigration answers the inherited half -- embed it and only the half
// that is yours is left to write. It is the arrangement query.BaseGrammar
// already uses for Illuminate's other abstract class.
//
// GetName has no PHP counterpart and had to exist: there, the name is the file
// name, read off the path by Migrator::getMigrationName. Here a migration is
// code, and code has no path to read -- so it says its own name, in the same
// format, and that string is what lands in the repository table.
type Migration interface {
	// GetName is the migration's identity: "2026_08_11_000000_create_users_table".
	//
	// The date prefix is not decoration. It is what makes two machines apply
	// the same migrations in the same order, and it is the whole of the
	// ordering rule -- the registry sorts by this string and nothing else.
	GetName() string

	// Up applies the change.
	Up(ctx context.Context, conn Connection) error

	// GetConnection answers Migration::getConnection: the connection this
	// migration runs on, empty for the default.
	GetConnection() string

	// ShouldRun answers Migration::shouldRun. A migration that answers false is
	// skipped and NOT recorded, so it is reconsidered on the next run.
	ShouldRun() bool

	// WithinTransaction answers Migration::$withinTransaction.
	//
	// It is a method rather than a field because Go's zero value for a bool is
	// false and the PHP's default is true: a struct field would turn every
	// migration that did not think about it into one that runs unprotected.
	WithinTransaction() bool
}

// ReversibleMigration is a Migration that can be rolled back.
//
// The PHP Migrator tests `method_exists($migration, $method)` before calling
// down(), so a migration without one is simply not reversed. A type assertion
// is Go's spelling of that test, and it happens at compile time for the
// migration and at run time for the Migrator -- which is strictly better than
// method_exists, because a Down with the wrong signature is a build failure
// rather than a rollback that silently does nothing.
type ReversibleMigration interface {
	Migration

	// Down reverses what Up applied.
	Down(ctx context.Context, conn Connection) error
}

// BaseMigration answers the concrete half of
// Illuminate\Database\Migrations\Migration: what a subclass inherits by
// extending it.
//
// A migration embeds it and writes GetName and Up:
//
//	type CreateUsersTable struct{ migrations.BaseMigration }
//
//	func (CreateUsersTable) GetName() string {
//	    return "2026_08_11_000000_create_users_table"
//	}
//
//	func (CreateUsersTable) Up(ctx context.Context, conn migrations.Connection) error {
//	    _, err := conn.Statement(ctx, `CREATE TABLE users (...)`, nil)
//	    return err
//	}
type BaseMigration struct {
	// Connection is Migration::$connection: the connection to run on, empty
	// for the default. It is exported because a migration sets it, and PHP
	// sets it by declaring `protected $connection = 'reporting';`.
	Connection string

	// OutsideTransaction inverts Migration::$withinTransaction.
	//
	// The PHP property defaults to true, and a Go bool defaults to false, so
	// carrying the name over would have made "I did not think about this" mean
	// "do not wrap this in a transaction" -- the opposite of what it means
	// there. The flag is inverted so the zero value is the PHP's default, and
	// the name says which way it points. Set it for the statement an engine
	// refuses inside a transaction: CREATE INDEX CONCURRENTLY is the one that
	// bites first.
	OutsideTransaction bool
}

// GetConnection answers Migration::getConnection.
func (m BaseMigration) GetConnection() string { return m.Connection }

// ShouldRun answers Migration::shouldRun, which is true unless a migration says
// otherwise.
func (m BaseMigration) ShouldRun() bool { return true }

// WithinTransaction answers Migration::$withinTransaction.
func (m BaseMigration) WithinTransaction() bool { return !m.OutsideTransaction }

// MigrationResult is how one migration turned out: Success, Failure or
// Skipped. Its String is the word the Migrator prints beside the migration's
// name.
//
// Skipped is the case worth knowing about, and it is not a failure: a migration
// whose ShouldRun answers false is passed over deliberately -- the guard exists
// for a migration that only applies to one engine, or one deployment -- and the
// run carries on. The numbers are the PHP's own, so a value written down by one
// side reads the same on the other.
//
// Answers Illuminate\Database\Migrations\MigrationResult, the backed enum the
// Migrator writes next to a migration's name.
type MigrationResult int

// The three cases of MigrationResult, with the PHP's own values.
const (
	// Success is MigrationResult::Success.
	Success MigrationResult = 1
	// Failure is MigrationResult::Failure.
	Failure MigrationResult = 2
	// Skipped is MigrationResult::Skipped, what ShouldRun answering false
	// prints.
	Skipped MigrationResult = 3
)

// String is the label the console prints. PHP gets it from the enum case name.
func (r MigrationResult) String() string {
	switch r {
	case Success:
		return "Success"
	case Failure:
		return "Failure"
	case Skipped:
		return "Skipped"
	default:
		return "Unknown"
	}
}
