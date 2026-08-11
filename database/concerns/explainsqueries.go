package concerns

import (
	"context"

	"github.com/arandu-io/hesape/auth"
)

// Explainable is what ExplainsQueries asks of the query it explains.
//
// The PHP trait reaches for $this->toSql(), $this->getBindings() and
// $this->getConnection(); those three are written out here, because a trait
// mixed into two different builders is an interface in Go.
type Explainable interface {
	// ToSQL answers Builder::toSql. The PHP spells it toSql.
	ToSQL() string

	// GetBindings answers Builder::getBindings.
	GetBindings() []any

	// GetConnection answers Builder::getConnection, narrowed to the one call
	// Explain makes on it.
	GetConnection() ExplainConnection
}

// ExplainConnection is the connection Explain runs the EXPLAIN through.
//
// A row is a map because EXPLAIN answers a different shape on every engine --
// Postgres one text column, MySQL twelve -- and a struct for it would be a
// struct for one of the three.
type ExplainConnection interface {
	// Select answers Connection::select. The Grant is required because this
	// executes: RULE 17 makes no exception for a query plan, and an EXPLAIN
	// that skipped the Policy would be a way to learn about rows the caller
	// may not read.
	Select(ctx context.Context, g auth.Grant, query string, bindings []any) ([]map[string]any, error)
}

// Explain answers Concerns\ExplainsQueries::explain: it asks the engine what it
// would do with this query, without running it.
//
// The PHP returns a Collection; this returns the rows and the error the select
// can fail with. The statement is the PHP's, verbatim: 'EXPLAIN '.$sql, with
// the query's own bindings.
func Explain(ctx context.Context, g auth.Grant, query Explainable) ([]map[string]any, error) {
	sql := query.ToSQL()

	bindings := query.GetBindings()

	return query.GetConnection().Select(ctx, g, "EXPLAIN "+sql, bindings)
}
