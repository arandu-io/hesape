package factories_test

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/model/factories"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
	"github.com/arandu-io/hesape/database/query/processors"
)

// The cost of a hundred rows, made and created.
//
// A factory row is built through the model, which is what makes a made row a row
// that can be saved, and what it costs is one allocation per row -- the model the
// row is built through. Creating pays it either way: it allocated the instances
// already, and it no longer builds a slice of values beside them.

// silentConnection answers everything and remembers nothing, so a hundred rows an
// iteration does not turn into a benchmark of a growing slice of statements.
type silentConnection struct{ lastID int64 }

func (c *silentConnection) Select(context.Context, string, []any, bool) ([]query.Record, error) {
	return nil, nil
}

func (c *silentConnection) Insert(context.Context, string, []any) (bool, error) {
	c.lastID++
	return true, nil
}

// GetLastInsertID satisfies processors.LastInsertIDConnection, which a model with
// an incrementing key reads the new key back through.
func (c *silentConnection) GetLastInsertID(string) (int64, error) { return c.lastID, nil }

func (c *silentConnection) Update(context.Context, string, []any) (int64, error) { return 1, nil }
func (c *silentConnection) Delete(context.Context, string, []any) (int64, error) { return 1, nil }

func (c *silentConnection) Statement(context.Context, string, []any) (bool, error) {
	return true, nil
}

func benchFactory() *factories.Factory[user] {
	m := model.NewModel[user]("users", &silentConnection{}, grammars.NewSQLiteGrammar(), processors.NewSQLiteProcessor())
	m.Timestamps = false
	return factories.For(m, definition)
}

func BenchmarkFactoryMake(b *testing.B) {
	f := benchFactory().Count(100)
	b.ReportAllocs()
	for b.Loop() {
		rows, err := f.Make()
		if err != nil {
			b.Fatal(err)
		}
		if len(rows) != 100 {
			b.Fatalf("made %d rows", len(rows))
		}
	}
}

func BenchmarkFactoryCreate(b *testing.B) {
	f := benchFactory().Count(100)
	ctx := context.Background()
	g := auth.SystemGrant("write", "acme")
	b.ReportAllocs()
	for b.Loop() {
		rows, err := f.Create(ctx, g)
		if err != nil {
			b.Fatal(err)
		}
		if len(rows) != 100 {
			b.Fatalf("created %d rows", len(rows))
		}
	}
}
