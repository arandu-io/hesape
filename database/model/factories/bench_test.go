package factories_test

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/query"
)

// The cost of a hundred rows, made and created.
//
// A factory row is built through the table, which is what makes a made row a
// row that can be saved: the entity and its bookkeeping, and the definition's
// values in it. Creating pays the statement on top.

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

func benchFactory() *userFactory {
	return newUserFactory(sqliteDB{&silentConnection{}}, definition)
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

var _ model.DB = sqliteDB{}
