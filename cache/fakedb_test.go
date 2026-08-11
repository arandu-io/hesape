package cache_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// A minimal database/sql driver holding the two cache tables in memory, so
// DatabaseStore can be exercised without a server. It is written here rather
// than pulled from a mock library because this collection has one dependency
// and a test dependency is still a dependency.
//
// It answers the statements DatabaseStore issues and nothing else. It is not a
// database and does not try to be one: it has no parser, it dispatches on the
// text of the query, and a statement it does not recognise is a failed test
// rather than a wrong answer -- which is the property that makes it worth
// having, because a store that changed its SQL would be caught here.

func init() { sql.Register("arandu-fake-cache", fakeDriver{}) }

type fakeDriver struct{}

func (fakeDriver) Open(dsn string) (driver.Conn, error) {
	fakeDBsMu.Lock()
	defer fakeDBsMu.Unlock()

	db, ok := fakeDBs[dsn]
	if !ok {
		return nil, fmt.Errorf("no fake database named %q", dsn)
	}
	return &fakeConn{db: db}, nil
}

var (
	fakeDBsMu sync.Mutex
	fakeDBs   = map[string]*fakeCacheDB{}
	fakeSeq   int
)

// fakeRow is one row of either table. The lock table leaves value empty and the
// cache table leaves owner empty, which is enough separation for two tables that
// are never joined.
type fakeRow struct {
	key        string
	value      []byte
	owner      string
	expiration int64
}

type fakeCacheDB struct {
	mu     sync.Mutex
	tables map[string]map[string]fakeRow

	// unknown records every statement the fake did not recognise, so a test can
	// fail on it instead of on the wrong answer it would otherwise give.
	unknown []string
}

// newFakeCacheDB returns a handle on two empty tables, named cache and
// cache_locks.
func newFakeCacheDB() (*sql.DB, *fakeCacheDB) {
	fakeDBsMu.Lock()
	fakeSeq++
	dsn := fmt.Sprintf("fake-cache-%d", fakeSeq)
	state := &fakeCacheDB{tables: map[string]map[string]fakeRow{
		"cache":       {},
		"cache_locks": {},
	}}
	fakeDBs[dsn] = state
	fakeDBsMu.Unlock()

	db, err := sql.Open("arandu-fake-cache", dsn)
	if err != nil {
		panic(err)
	}
	return db, state
}

func (d *fakeCacheDB) rows(table string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.tables[table])
}

func (d *fakeCacheDB) unrecognised() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.unknown...)
}

type fakeConn struct{ db *fakeCacheDB }

func (c *fakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("the fake cache database answers only through the context methods")
}
func (c *fakeConn) Close() error              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) { return fakeTx{}, nil }

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

var (
	_ driver.ExecerContext  = (*fakeConn)(nil)
	_ driver.QueryerContext = (*fakeConn)(nil)
)

// errDuplicateKey is what a primary key violation looks like to DatabaseStore,
// which reads it as "somebody else got there first".
var errDuplicateKey = errors.New("fake cache database: duplicate key")

func (c *fakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	values := plainValues(args)
	table := tableOf(query)

	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	rows := c.db.tables[table]

	switch {
	case strings.HasPrefix(query, "INSERT INTO ") && strings.Contains(query, "(key, value, expiration)"):
		key := asString(values[0])
		if _, exists := rows[key]; exists {
			return nil, errDuplicateKey
		}
		rows[key] = fakeRow{key: key, value: asBytes(values[1]), expiration: asInt(values[2])}
		return fakeResult(1), nil

	case strings.HasPrefix(query, "INSERT INTO ") && strings.Contains(query, "(key, owner, expiration)"):
		key := asString(values[0])
		if _, exists := rows[key]; exists {
			return nil, errDuplicateKey
		}
		rows[key] = fakeRow{key: key, owner: asString(values[1]), expiration: asInt(values[2])}
		return fakeResult(1), nil

	case strings.Contains(query, "SET value = ?, expiration = ? WHERE key = ?"):
		key := asString(values[2])
		row, ok := rows[key]
		if !ok {
			return fakeResult(0), nil
		}
		row.value, row.expiration = asBytes(values[0]), asInt(values[1])
		rows[key] = row
		return fakeResult(1), nil

	case strings.Contains(query, "SET value = ? WHERE key = ? AND value = ?"):
		key := asString(values[1])
		row, ok := rows[key]
		if !ok || string(row.value) != string(asBytes(values[2])) {
			return fakeResult(0), nil
		}
		row.value = asBytes(values[0])
		rows[key] = row
		return fakeResult(1), nil

	case strings.Contains(query, "SET expiration = ? WHERE key = ? AND expiration > ?"):
		key := asString(values[1])
		row, ok := rows[key]
		if !ok || row.expiration <= asInt(values[2]) {
			return fakeResult(0), nil
		}
		row.expiration = asInt(values[0])
		rows[key] = row
		return fakeResult(1), nil

	case strings.Contains(query, "SET owner = ?, expiration = ? WHERE key = ? AND (owner = ? OR expiration <= ?)"):
		key := asString(values[2])
		row, ok := rows[key]
		if !ok {
			return fakeResult(0), nil
		}
		if row.owner != asString(values[3]) && row.expiration > asInt(values[4]) {
			return fakeResult(0), nil
		}
		row.owner, row.expiration = asString(values[0]), asInt(values[1])
		rows[key] = row
		return fakeResult(1), nil

	case strings.Contains(query, "WHERE key IN (?, ?) AND expiration <= ?"):
		deleted := 0
		limit := asInt(values[2])
		for _, v := range values[:2] {
			key := asString(v)
			if row, ok := rows[key]; ok && row.expiration <= limit {
				delete(rows, key)
				deleted++
			}
		}
		return fakeResult(int64(deleted)), nil

	case strings.Contains(query, "WHERE key IN (?, ?)"):
		deleted := 0
		for _, v := range values {
			key := asString(v)
			if _, ok := rows[key]; ok {
				delete(rows, key)
				deleted++
			}
		}
		return fakeResult(int64(deleted)), nil

	case strings.Contains(query, "WHERE key = ? AND owner = ?"):
		key := asString(values[0])
		if row, ok := rows[key]; ok && row.owner == asString(values[1]) {
			delete(rows, key)
			return fakeResult(1), nil
		}
		return fakeResult(0), nil

	case strings.Contains(query, "WHERE key LIKE ?"):
		prefix := unescapeLike(strings.TrimSuffix(asString(values[0]), "%"))
		deleted := 0
		for key := range rows {
			if strings.HasPrefix(key, prefix) {
				delete(rows, key)
				deleted++
			}
		}
		return fakeResult(int64(deleted)), nil

	case strings.Contains(query, "WHERE expiration <= ?"):
		limit := asInt(values[0])
		deleted := 0
		for key, row := range rows {
			if row.expiration <= limit {
				delete(rows, key)
				deleted++
			}
		}
		return fakeResult(int64(deleted)), nil

	case strings.HasPrefix(query, "DELETE FROM ") && !strings.Contains(query, "WHERE"):
		deleted := int64(len(rows))
		clear(rows)
		return fakeResult(deleted), nil
	}

	c.db.unknown = append(c.db.unknown, query)
	return nil, fmt.Errorf("fake cache database: unrecognised statement %q", query)
}

func (c *fakeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	values := plainValues(args)
	table := tableOf(query)

	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	rows := c.db.tables[table]

	switch {
	case strings.HasPrefix(query, "SELECT value, expiration FROM "):
		row, ok := rows[asString(values[0])]
		if !ok {
			return &fakeRows{columns: []string{"value", "expiration"}}, nil
		}
		return &fakeRows{
			columns: []string{"value", "expiration"},
			values:  [][]driver.Value{{row.value, row.expiration}},
		}, nil

	case strings.HasPrefix(query, "SELECT owner, expiration FROM "):
		row, ok := rows[asString(values[0])]
		if !ok {
			return &fakeRows{columns: []string{"owner", "expiration"}}, nil
		}
		return &fakeRows{
			columns: []string{"owner", "expiration"},
			values:  [][]driver.Value{{row.owner, row.expiration}},
		}, nil

	case strings.HasPrefix(query, "SELECT key, value, expiration FROM "):
		out := &fakeRows{columns: []string{"key", "value", "expiration"}}
		for _, v := range values {
			if row, ok := rows[asString(v)]; ok {
				out.values = append(out.values, []driver.Value{row.key, row.value, row.expiration})
			}
		}
		return out, nil
	}

	c.db.unknown = append(c.db.unknown, query)
	return nil, fmt.Errorf("fake cache database: unrecognised query %q", query)
}

// tableOf reads the table name out of a statement, which is the whole of the
// parsing this fake does.
func tableOf(query string) string {
	for _, keyword := range []string{" FROM ", "INSERT INTO ", "UPDATE "} {
		i := strings.Index(query, keyword)
		if i < 0 {
			continue
		}
		rest := query[i+len(keyword):]
		if j := strings.IndexAny(rest, " ("); j >= 0 {
			return rest[:j]
		}
		return strings.TrimSpace(rest)
	}
	return ""
}

// unescapeLike undoes the escaping likePrefix applies, so the fake can compare
// the pattern as a plain prefix.
func unescapeLike(pattern string) string {
	return strings.NewReplacer(`\%`, "%", `\_`, "_", `\\`, `\`).Replace(pattern)
}

type fakeResult int64

func (r fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return int64(r), nil }

type fakeRows struct {
	columns []string
	values  [][]driver.Value
	at      int
}

func (r *fakeRows) Columns() []string { return r.columns }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.at >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.at])
	r.at++
	return nil
}

func plainValues(args []driver.NamedValue) []driver.Value {
	out := make([]driver.Value, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

func asString(v driver.Value) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

func asBytes(v driver.Value) []byte {
	switch t := v.(type) {
	case []byte:
		return append([]byte(nil), t...)
	case string:
		return []byte(t)
	default:
		return []byte(fmt.Sprint(t))
	}
}

func asInt(v driver.Value) int64 {
	if n, ok := v.(int64); ok {
		return n
	}
	return 0
}
