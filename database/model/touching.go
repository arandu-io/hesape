package model

import (
	"context"
	"slices"
	"sync"

	"github.com/arandu-io/hesape/auth"
)

// ignoreOnTouch holds the tables for which touch propagation is currently
// suspended.
//
// It is a package variable behind a mutex, the same shape strict.go uses for its
// switches: the state is process-wide rather than per-row, so any goroutine
// calling WithoutTouchingOn or IsIgnoringTouch sees the same list.
var ignoreOnTouch struct {
	mu     sync.RWMutex
	tables []*Table
}

// WithoutTouching suspends touch propagation for the table for the length of
// callback: a relation whose owner would have had its updated_at bumped is left
// alone until callback returns.
func (t *Table) WithoutTouching(callback func() error) error {
	return WithoutTouchingOn([]*Table{t}, callback)
}

// WithoutTouchingOn suspends touch propagation for every table in tables for
// the length of callback, restoring the previous list once callback returns --
// even when it returns an error.
func WithoutTouchingOn(tables []*Table, callback func() error) error {
	ignoreOnTouch.mu.Lock()
	ignoreOnTouch.tables = append(ignoreOnTouch.tables, tables...)
	ignoreOnTouch.mu.Unlock()

	defer func() {
		ignoreOnTouch.mu.Lock()
		defer ignoreOnTouch.mu.Unlock()
		ignoreOnTouch.tables = slices.DeleteFunc(ignoreOnTouch.tables, func(t *Table) bool {
			return slices.Contains(tables, t)
		})
	}()

	return callback()
}

// IsIgnoringTouch reports whether touch propagation is currently suspended for
// the table.
//
// A table with no updated_at column, or with timestamps switched off, reports
// true without consulting the suspended list, because neither has anything a
// touch could update.
func (t *Table) IsIgnoringTouch() bool {
	if t.updatedAt == "" || !t.timestamps {
		return true
	}
	ignoreOnTouch.mu.RLock()
	defer ignoreOnTouch.mu.RUnlock()
	return slices.Contains(ignoreOnTouch.tables, t)
}

// Touch stamps the row's updated-at column and saves it.
//
// A table that does not use timestamps, or has no updated-at column, is not an
// error: it is a row there is nothing to stamp on, and the call is a no-op
// rather than a failure. The builder's Touch is the same idea over a set of
// rows.
func (m *Model) Touch(ctx context.Context, g auth.Grant) error {
	if err := wired(m); err != nil {
		return err
	}
	if !usesTimestamps(m) || m.r.table.updatedAt == "" {
		return nil
	}
	if err := m.SetAttribute(m.r.table.updatedAt, freshTimestamp()); err != nil {
		return err
	}
	_, err := m.Save(ctx, g)
	return err
}

// touches reports whether saving this row stamps the owner of relation.
//
// The list is the table's, from TableSpec.Touches. An empty list means the row
// touches nothing, which is the default.
func touches(m *Model, relation string) bool {
	return slices.Contains(m.r.table.touches, relation)
}
