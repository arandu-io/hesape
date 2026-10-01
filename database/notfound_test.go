package database_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/concerns"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/model/relations"
	"github.com/arandu-io/hesape/database/query"
)

// TestEveryNotFoundIsARecordNotFound: a routing layer answers a missing row
// with 404 by checking database.ErrRecordNotFound. Before the sentinels were
// joined, a miss from the query builder, from a repository's Find or from a
// relation matched nothing it checked, and became a 500.
func TestEveryNotFoundIsARecordNotFound(t *testing.T) {
	cases := map[string]error{
		"query.ErrRecordNotFound":      query.ErrRecordNotFound,
		"query.ErrRecordsNotFound":     query.ErrRecordsNotFound,
		"concerns.ErrRecordNotFound":   concerns.ErrRecordNotFound,
		"concerns.ErrRecordsNotFound":  concerns.ErrRecordsNotFound,
		"database.ErrRecordsNotFound":  database.ErrRecordsNotFound,
		"database.ErrNotFound":         database.ErrNotFound,
		"model.ErrModelNotFound":       model.ErrModelNotFound,
		"relations.ErrModelNotFound":   relations.ErrModelNotFound,
		"a wrapped repository miss":    fmt.Errorf("%w: invoice inv-1", database.ErrNotFound),
		"a wrapped relation miss":      fmt.Errorf("%w: table posts", relations.ErrModelNotFound),
		"a model.ModelNotFoundError":   &model.ModelNotFoundError{Model: "users", IDs: []any{7}},
		"a wrapped FirstOrFail miss":   fmt.Errorf("loading: %w", query.ErrRecordNotFound),
		"a wrapped Sole miss":          fmt.Errorf("loading: %w", database.ErrRecordsNotFound),
		"a ModelNotFoundError wrapped": fmt.Errorf("show: %w", &model.ModelNotFoundError{Model: "users"}),
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			if !errors.Is(err, database.ErrRecordNotFound) {
				t.Fatalf("errors.Is(%v, database.ErrRecordNotFound) = false; a routing layer would answer it with 500", err)
			}
		})
	}
}

// TestTheRecordNotFoundNamesAreOneValue: FirstOrFail lives in query and in
// concerns, and database re-exports the second. A miss from either must match
// under every name, in both directions, which only one value guarantees.
func TestTheRecordNotFoundNamesAreOneValue(t *testing.T) {
	names := []error{query.ErrRecordNotFound, concerns.ErrRecordNotFound, database.ErrRecordNotFound}
	for _, a := range names {
		for _, b := range names {
			if !errors.Is(a, b) {
				t.Errorf("errors.Is(%v, %v) = false", a, b)
			}
		}
	}

	records := []error{query.ErrRecordsNotFound, concerns.ErrRecordsNotFound, database.ErrRecordsNotFound}
	for _, a := range records {
		for _, b := range records {
			if !errors.Is(a, b) {
				t.Errorf("errors.Is(%v, %v) = false", a, b)
			}
		}
	}
}

// TestTheNarrowSentinelsStayDistinguishable: joining the not-found sentinels
// must not make the broad one match the narrow ones, or a caller that told a
// repository's miss from a FirstOrFail's, or Sole's none from FirstOrFail's,
// would lose the distinction.
func TestTheNarrowSentinelsStayDistinguishable(t *testing.T) {
	cases := []struct {
		err, notIs error
	}{
		{database.ErrRecordNotFound, database.ErrNotFound},
		{database.ErrRecordNotFound, database.ErrRecordsNotFound},
		{database.ErrRecordNotFound, model.ErrModelNotFound},
		{database.ErrNotFound, database.ErrRecordsNotFound},
		{database.ErrNotFound, model.ErrModelNotFound},
		{model.ErrModelNotFound, database.ErrNotFound},
		{database.ErrRecordsNotFound, model.ErrModelNotFound},
	}
	for _, c := range cases {
		if errors.Is(c.err, c.notIs) {
			t.Errorf("errors.Is(%v, %v) = true, want false", c.err, c.notIs)
		}
	}
}

// TestTheRepositoryMissKeepsItsMessage: the sentinel matches more than it did,
// and says the same thing in a log line.
func TestTheRepositoryMissKeepsItsMessage(t *testing.T) {
	if got := database.ErrNotFound.Error(); got != "database: no such row" {
		t.Fatalf("ErrNotFound.Error() = %q", got)
	}
	if got := query.ErrRecordsNotFound.Error(); got != "query: no records found for the given query" {
		t.Fatalf("ErrRecordsNotFound.Error() = %q", got)
	}
}
