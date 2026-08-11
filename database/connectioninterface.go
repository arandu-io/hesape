package database

import (
	"context"

	"github.com/arandu-io/hesape/database/query"
)

// ConnectionInterface answers Illuminate\Database\ConnectionInterface.
//
// Every method the PHP declares is here, with two mechanical changes ADR 0044
// allows: a context.Context first, because a statement that cannot be cancelled
// holds a server connection for as long as the server likes; and an error
// return wherever the PHP lets a PDOException travel out unannounced.
//
// A caller that holds one of these is below the authorization layer, not
// outside it -- see the Connection doc for where the Grant lives and why it is
// not on these methods.
type ConnectionInterface interface {
	// Table answers ConnectionInterface::table.
	Table(ctx context.Context, table any, as ...string) *query.Builder

	// Query is Connection::query. It is not on the PHP interface, and is here
	// because Table is unusable without it: PHP reaches the builder through the
	// DB facade, which ADR 0002 removed.
	Query(ctx context.Context) *query.Builder

	// Raw answers ConnectionInterface::raw.
	Raw(value any) query.Expression

	// SelectOne answers ConnectionInterface::selectOne. The bool is the PHP's
	// null: a nil record and "there was no row" are the same value in Go
	// otherwise.
	SelectOne(ctx context.Context, query string, bindings []any, useReadPDO bool) (query.Record, bool, error)

	// Scalar answers ConnectionInterface::scalar.
	Scalar(ctx context.Context, query string, bindings []any, useReadPDO bool) (any, error)

	// Select answers ConnectionInterface::select.
	Select(ctx context.Context, query string, bindings []any, useReadPDO bool) ([]query.Record, error)

	// Cursor answers ConnectionInterface::cursor. The PHP returns a Generator;
	// this returns a range-over-func iterator, which is the same thing.
	Cursor(ctx context.Context, query string, bindings []any, useReadPDO bool) func(yield func(query.Record, error) bool)

	// Insert answers ConnectionInterface::insert.
	Insert(ctx context.Context, query string, bindings []any) (bool, error)

	// Update answers ConnectionInterface::update.
	Update(ctx context.Context, query string, bindings []any) (int64, error)

	// Delete answers ConnectionInterface::delete.
	Delete(ctx context.Context, query string, bindings []any) (int64, error)

	// Statement answers ConnectionInterface::statement.
	Statement(ctx context.Context, query string, bindings []any) (bool, error)

	// AffectingStatement answers ConnectionInterface::affectingStatement.
	AffectingStatement(ctx context.Context, query string, bindings []any) (int64, error)

	// Unprepared answers ConnectionInterface::unprepared.
	Unprepared(ctx context.Context, query string) (bool, error)

	// PrepareBindings answers ConnectionInterface::prepareBindings.
	PrepareBindings(bindings []any) []any

	// Transaction answers ConnectionInterface::transaction. The PHP returns
	// whatever the callback returned; a Go method cannot be generic, so the
	// callback carries its result out through the closure.
	Transaction(callback func() error, attempts int) error

	// BeginTransaction answers ConnectionInterface::beginTransaction.
	BeginTransaction() error

	// Commit answers ConnectionInterface::commit.
	Commit() error

	// RollBack answers ConnectionInterface::rollBack. Nil is the PHP's null
	// default: back one level.
	RollBack(toLevel *int) error

	// TransactionLevel answers ConnectionInterface::transactionLevel.
	TransactionLevel() int

	// Pretend answers ConnectionInterface::pretend.
	Pretend(ctx context.Context, callback func(*Connection) error) ([]QueryLogEntry, error)

	// GetDatabaseName answers ConnectionInterface::getDatabaseName.
	GetDatabaseName() string
}

// Connection satisfies the interface it answers to. The PHP says so with
// `implements ConnectionInterface`; Go says it here, at compile time, which is
// the only place a claim like that is worth making.
var _ ConnectionInterface = (*Connection)(nil)
