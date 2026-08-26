package factories_test

import (
	"context"
	"strings"
	"sync"

	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
	"github.com/arandu-io/hesape/database/query/processors"
)

// recordingConnection is what the relation tests write through: it answers
// nothing and remembers the order it was asked in, which is the property those
// tests are about.
type recordingConnection struct {
	mu         sync.Mutex
	statements []string
	lastID     int64
}

func newRecordingConnection() *recordingConnection { return &recordingConnection{} }

func (c *recordingConnection) record(sql string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements = append(c.statements, sql)
}

// inserts counts the insert statements that reached the connection.
func (c *recordingConnection) inserts() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, sql := range c.statements {
		if strings.HasPrefix(strings.ToLower(sql), "insert") {
			n++
		}
	}
	return n
}

// first names the table of the first statement, which is how the ordering of a
// parent against its children is read.
func (c *recordingConnection) first() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, sql := range c.statements {
		lowered := strings.ToLower(sql)
		if !strings.HasPrefix(lowered, "insert") {
			continue
		}
		if strings.Contains(lowered, "users") {
			return "users"
		}
		if strings.Contains(lowered, "posts") {
			return "posts"
		}
	}
	return ""
}

func (c *recordingConnection) Select(_ context.Context, sql string, _ []any, _ bool) ([]query.Record, error) {
	c.record(sql)
	return nil, nil
}

func (c *recordingConnection) Insert(_ context.Context, sql string, _ []any) (bool, error) {
	c.record(sql)
	c.mu.Lock()
	c.lastID++
	c.mu.Unlock()
	return true, nil
}

// GetLastInsertID satisfies processors.LastInsertIDConnection. A model with an
// incrementing key reads it back after the insert, and a child that names its
// parent needs the number that came back rather than the zero it went in with.
func (c *recordingConnection) GetLastInsertID(string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastID, nil
}

func (c *recordingConnection) Update(_ context.Context, sql string, _ []any) (int64, error) {
	c.record(sql)
	return 1, nil
}

func (c *recordingConnection) Delete(_ context.Context, sql string, _ []any) (int64, error) {
	c.record(sql)
	return 1, nil
}

func (c *recordingConnection) Statement(_ context.Context, sql string, _ []any) (bool, error) {
	c.record(sql)
	return true, nil
}

// newModelOn is the model the factory writes through.
func newModelOn[T any](conn query.Connection, table string) *model.Model[T] {
	m := model.NewModel[T](table, conn, grammars.NewSQLiteGrammar(), processors.NewSQLiteProcessor())
	m.Timestamps = false
	return m
}
