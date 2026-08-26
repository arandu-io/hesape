package eloquent

import (
	"fmt"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// The baseline.
//
// This package had no benchmark at all, and the hydration path is about to be
// rewritten. A rewrite with no before-number cannot report a win: it can only
// report a feeling. So these run first, against the code as it stands, and the
// same file measures the after.
//
// The number that matters is not BenchmarkHydrate on its own -- it is the ratio
// between it and BenchmarkHandScan, which does the same work the way a
// hand-written repository does it. That ratio is what a reader is entitled to
// ask about before putting an ORM on the hot path, and it is the only figure
// that stays meaningful across machines.

// benchRows builds n records shaped like the rows a select on users returns.
//
// The values are the ones a driver actually hands back -- int64 for an integer
// column, time.Time for a timestamp -- so the conversions being measured are
// the conversions that happen in production, not the fast path that a
// hand-tuned fixture would accidentally take.
func benchRows(n int) []query.Record {
	now := time.Now().UTC()
	out := make([]query.Record, 0, n)
	for i := range n {
		out = append(out, query.Record{
			"id":         int64(i + 1),
			"name":       fmt.Sprintf("user %d", i),
			"email":      fmt.Sprintf("user%d@example.test", i),
			"tenant_id":  "acme",
			"created_at": now,
			"updated_at": now,
			"deleted_at": nil,
		})
	}
	return out
}

// handScan is the comparison: the same rows into the same struct, written the
// way the repository templates write it. No reflection, no attribute map, no
// original-value bookkeeping.
func handScan(rows []query.Record) []user {
	out := make([]user, 0, len(rows))
	for _, row := range rows {
		var u user
		if v, ok := row["id"].(int64); ok {
			u.ID = v
		}
		if v, ok := row["name"].(string); ok {
			u.Name = v
		}
		if v, ok := row["email"].(string); ok {
			u.Email = v
		}
		if v, ok := row["tenant_id"].(string); ok {
			u.TenantID = v
		}
		if v, ok := row["created_at"].(time.Time); ok {
			u.CreatedAt = v
		}
		if v, ok := row["updated_at"].(time.Time); ok {
			u.UpdatedAt = v
		}
		if v, ok := row["deleted_at"].(time.Time); ok {
			u.DeletedAt = &v
		}
		out = append(out, u)
	}
	return out
}

var benchSizes = []int{1, 100, 1000, 10000}

func BenchmarkHydrate(b *testing.B) {
	for _, size := range benchSizes {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			model, _ := newUserModel()
			builder := model.NewQuery()
			rows := benchRows(size)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := builder.Hydrate(rows); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkHandScan(b *testing.B) {
	for _, size := range benchSizes {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			rows := benchRows(size)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_ = handScan(rows)
			}
		})
	}
}

// BenchmarkNewFromBuilder is one row on its own: the per-row cost with the
// slice growth of Hydrate taken out of it.
func BenchmarkNewFromBuilder(b *testing.B) {
	model, _ := newUserModel()
	row := benchRows(1)[0]

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := model.NewFromBuilder(row); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkNewInstance measures what a hydrated row pays before a single
// column is written: the clone of the events map, the global scopes and the
// three visibility slices.
func BenchmarkNewInstance(b *testing.B) {
	model, _ := newUserModel()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := model.NewInstance(nil, true); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetAttributes is the write path's first step: struct to map, by
// reflection, on every call with no memoization.
func BenchmarkGetAttributes(b *testing.B) {
	model, _ := newUserModel()
	instance, err := model.NewFromBuilder(benchRows(1)[0])
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = instance.GetAttributes()
	}
}

// BenchmarkGetDirty is the quadratic one: GetAttributes walks the struct, and
// then every key is looked up again through a linear scan over the field slice.
func BenchmarkGetDirty(b *testing.B) {
	model, _ := newUserModel()
	instance, err := model.NewFromBuilder(benchRows(1)[0])
	if err != nil {
		b.Fatal(err)
	}
	if err := instance.SetAttribute("name", "changed"); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = instance.GetDirty()
	}
}

// BenchmarkSaveExisting is the four-walk path: IsDirty, GetDirty, SyncChanges
// through GetDirty again, and SyncOriginal through GetAttributes.
func BenchmarkSaveExisting(b *testing.B) {
	model, conn := newUserModel()
	g := auth.SystemGrant("users.write", "acme")

	instance, err := model.NewFromBuilder(benchRows(1)[0])
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if err := instance.SetAttribute("name", fmt.Sprint("name ", i)); err != nil {
			b.Fatal(err)
		}
		if _, err := instance.Save(g); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	_ = conn
}

// BenchmarkSaveNew is the insert path, which does not consult the original
// values at all -- the difference between the two is the bookkeeping.
func BenchmarkSaveNew(b *testing.B) {
	model, _ := newUserModel()
	g := auth.SystemGrant("users.write", "acme")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		instance, err := model.NewInstance(nil, false)
		if err != nil {
			b.Fatal(err)
		}
		if err := instance.Fill(map[string]any{
			"name":  fmt.Sprint("user ", i),
			"email": fmt.Sprint("user", i, "@example.test"),
		}); err != nil {
			b.Fatal(err)
		}
		if _, err := instance.Save(g); err != nil {
			b.Fatal(err)
		}
	}
}
