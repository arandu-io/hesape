package console

import (
	"context"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
)

// CreateCacheTable creates the two tables [cache.DatabaseStore] reads and writes.
//
// It is code rather than a file in a tree, for the reason
// [migrations.Migration] gives: a migration written as SQL text describes one
// engine, and this collection runs on three.
type CreateCacheTable struct{ migrations.BaseMigration }

// GetName returns the migration's name.
func (CreateCacheTable) GetName() string {
	return "2026_08_10_000002_create_cache_table"
}

// Up creates both tables and the two indexes pruning reads.
//
// The key column is called key, which is reserved in MySQL. That is not a
// problem here and was one before: the Blueprint quotes every identifier it
// writes, so the column comes out as `key` on MySQL and "key" on the other two.
// The hand-written statement this replaced quoted nothing and would have been
// refused by MySQL on the first line.
//
// expiration is unix milliseconds, not seconds: a cache whose resolution is a
// second cannot honour a ttl shorter than one. See [cache.DatabaseStore].
func (CreateCacheTable) Up(ctx context.Context, conn migrations.Connection) error {
	if err := conn.Schema().Create(ctx, cache.Table, func(table *schema.Blueprint) {
		table.String("key").Primary()
		table.Text("value")
		table.BigInteger("expiration")

		// The index is what makes pruning the expired rows a scan of the ones
		// that are, rather than of the whole table.
		table.Index([]string{"expiration"}, "cache_expiration_index")
	}); err != nil {
		return err
	}

	return conn.Schema().Create(ctx, cache.LockTable, func(table *schema.Blueprint) {
		table.String("key").Primary()
		table.String("owner")
		table.BigInteger("expiration")

		table.Index([]string{"expiration"}, "cache_locks_expiration_index")
	})
}

// Down drops both tables, and the indexes with them.
func (CreateCacheTable) Down(ctx context.Context, conn migrations.Connection) error {
	if err := conn.Schema().DropIfExists(ctx, cache.LockTable); err != nil {
		return err
	}
	return conn.Schema().DropIfExists(ctx, cache.Table)
}

// Migrations is the cache tables.
//
// It is here rather than in the cache package, and that is a constraint rather
// than a preference: database/migrations tests its own isolation against a real
// cache lock, so a cache that imported database/migrations would close a cycle.
// The table names stay in cache, where the store reads them.
//
// An application caching in Redis never calls this. One using
// [cache.DatabaseStore] hands the result to the migrator.
func Migrations() []migrations.Migration {
	return []migrations.Migration{CreateCacheTable{}}
}
