package database

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// Presence is the count the `unique` and `exists` validation rules ask of the
// database, scoped to the tenant of the Grant the validation runs under.
//
// Its Count has the shape of validation.PresenceQuery, so the method value is
// what the verifier is built from, with nothing written in between:
//
//	presence := database.NewPresence(db, "users")
//	verifier := validation.NewDatabasePresenceVerifier(presence.Count)
//
// This package does not import validation, and does not need to: a method
// value whose signature is identical to a named function type is assignable
// to it.
//
// # The tenant
//
// A count is a read, and a read is filtered by auth.Tenant(g) like any other:
// `unique:invoices,number` asks whether this customer already has the number,
// not whether somebody does. The filter is query.TenantColumn, the column
// every statement the query builder issues is scoped by. A Grant that carries
// no tenant is refused with auth.ErrForbidden before any statement is built,
// and the validation rule then fails closed.
//
// A table shared by every tenant -- the accounts an e-mail address has to be
// unique across, typically -- is named to NewPresence, and is counted without
// the tenant filter. It is still counted only under a Grant that carries a
// tenant: dropping the filter is a statement about the table, not about who
// may ask.
type Presence struct {
	db     *DB
	global map[string]bool
}

// NewPresence returns the presence count over db. The tables named in global
// are shared by every tenant and are counted without the tenant filter; every
// other table is filtered by the Grant's tenant.
func NewPresence(db *DB, global ...string) *Presence {
	shared := make(map[string]bool, len(global))
	for _, table := range global {
		shared[table] = true
	}
	return &Presence{db: db, global: shared}
}

// Count answers how many distinct values of column, among values, rows of
// collection hold once the conditions are applied: the Grant's tenant, unless
// collection was named global; the row whose idColumn is excludeID left out,
// when excludeID is given ("id" when idColumn is empty); and every extra
// condition.
//
// An extra condition is a column and a value, read the way the rule strings
// write them: "NULL" is IS NULL, "NOT_NULL" is IS NOT NULL, a value starting
// with "!" is a not-equal on the rest, and anything else is an equality. The
// conditions are applied in the order of their columns, so one rule always
// issues the same statement.
//
// Distinct values rather than rows, because that is the question both rules
// ask: `exists` over a list passes when every value it was given is present,
// and a value held by two rows is still one value.
//
// The table and column names are the rule's parameters, which the application
// writes; they are quoted by the dialect's grammar, and every value travels as
// a placeholder.
func (p *Presence) Count(ctx context.Context, g auth.Grant, collection, column string, values []any,
	excludeID any, idColumn string, extra map[string]string) (int, error) {
	if p == nil || p.db == nil {
		return 0, errors.New("database: the presence count was built with no database handle")
	}
	tenant := auth.Tenant(g)
	if tenant == "" {
		return 0, fmt.Errorf("%w: counting %s.%s for a validation rule needs a Grant that carries a tenant", auth.ErrForbidden, collection, column)
	}
	if collection == "" || column == "" {
		return 0, fmt.Errorf("database: a presence count needs a table and a column, got %q and %q", collection, column)
	}
	if len(values) == 0 {
		return 0, nil
	}

	q := query.NewBuilder(p.db, p.db.GetQueryGrammar(), p.db.GetPostProcessor()).From(collection)
	if !p.global[collection] {
		q.Where(query.TenantColumn, "=", tenant)
	}
	q.WhereIn(column, values)
	if excludeID != nil && excludeID != "NULL" {
		if idColumn == "" {
			idColumn = "id"
		}
		q.Where(idColumn, "<>", excludeID)
	}
	columns := make([]string, 0, len(extra))
	for name := range extra {
		columns = append(columns, name)
	}
	slices.Sort(columns)
	for _, name := range columns {
		switch value := extra[name]; {
		case value == "NULL":
			q.WhereNull(name)
		case value == "NOT_NULL":
			q.WhereNotNull(name)
		case strings.HasPrefix(value, "!"):
			q.Where(name, "!=", value[1:])
		default:
			q.Where(name, "=", value)
		}
	}
	q.Distinct()
	q.SetAggregate("count", []any{column})

	if err := q.ScopeNested(ctx, g); err != nil {
		return 0, err
	}
	statement := q.ToSQL()
	if err := q.Err(); err != nil {
		return 0, err
	}

	var count int64
	if err := p.db.QueryRowContext(ctx, statement, p.db.prepareBindings(q.GetBindings())...).Scan(&count); err != nil {
		return 0, fmt.Errorf("database: counting %s.%s: %w", collection, column, err)
	}
	return int(count), nil
}
