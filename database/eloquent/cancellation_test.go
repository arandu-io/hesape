package eloquent

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// The context reaches the connection, which is the whole point of it being in
// the signature.
//
// It used to be in the signature and reach nothing. There were two doors: the
// one every terminal method declared, which was read once for a ctx.Err()
// pre-flight and then dropped, and a second one welded onto the builder at
// construction, which is the one that actually got to the driver. So the
// context a handler passed was not the context the statement ran under -- and
// for a model, which holds its connection as a field, the one that ran was
// whatever context existed when the model was built.
//
// A pre-flight check is not cancellation. It answers "was this already
// cancelled a moment ago", and the statement it guards then runs to completion
// however long the server takes. These tests tell the two apart by cancelling
// AFTER the check would have passed.

// cancellingConnection reports the context it was handed, on every call.
//
// It refuses only when the context is already done, which is what a driver
// does. The point is not the refusal -- it is that the connection is holding a
// context at all, and that it is the caller's.
type cancellingConnection struct {
	seen []context.Context
}

func (c *cancellingConnection) record(ctx context.Context) error {
	c.seen = append(c.seen, ctx)
	return ctx.Err()
}

func (c *cancellingConnection) Select(ctx context.Context, _ string, _ []any, _ bool) ([]query.Record, error) {
	return nil, c.record(ctx)
}

func (c *cancellingConnection) Insert(ctx context.Context, _ string, _ []any) (bool, error) {
	return false, c.record(ctx)
}

func (c *cancellingConnection) Update(ctx context.Context, _ string, _ []any) (int64, error) {
	return 0, c.record(ctx)
}

func (c *cancellingConnection) Delete(ctx context.Context, _ string, _ []any) (int64, error) {
	return 0, c.record(ctx)
}

func (c *cancellingConnection) Statement(ctx context.Context, _ string, _ []any) (bool, error) {
	return false, c.record(ctx)
}

// TestAReadCarriesTheCallersContextToTheConnection cancels the context after
// the builder exists and before the read runs.
//
// The pre-flight check would catch this one too, so on its own it proves only
// half. TestTheConnectionSeesTheContextTheCallerPassed is the other half.
func TestAReadCarriesTheCallersContextToTheConnection(t *testing.T) {
	conn := &cancellingConnection{}
	model := NewModel[user]("users", conn, newTestGrammar(), &testProcessor{})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := model.NewQuery().Where("name", "Ada").Get(ctx, auth.SystemGrant("users.read", "acme"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Get returned %v, want context.Canceled", err)
	}
}

// TestTheConnectionSeesTheContextTheCallerPassed is the assertion that a
// pre-flight check cannot pass.
//
// The context is live for the whole call, so nothing refuses anything. What is
// checked is the identity of the value the connection was handed: it has to be
// the one the caller passed, not one the model was built with and not a
// Background fabricated on the way down.
func TestTheConnectionSeesTheContextTheCallerPassed(t *testing.T) {
	conn := &cancellingConnection{}
	model := NewModel[user]("users", conn, newTestGrammar(), &testProcessor{})

	type key struct{}
	ctx := context.WithValue(t.Context(), key{}, "the caller's")

	if _, err := model.NewQuery().Get(ctx, auth.SystemGrant("users.read", "acme")); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(conn.seen) == 0 {
		t.Fatal("the connection was never called")
	}
	for i, seen := range conn.seen {
		if seen.Value(key{}) != "the caller's" {
			t.Errorf("statement %d ran under a context the caller did not pass", i)
		}
	}
}

// TestAWriteCarriesTheCallersContextToTheConnection is the same property on the
// write side, which is where a statement that outlives its request costs the
// most: a cancelled request that still inserts is a row nobody asked for.
func TestAWriteCarriesTheCallersContextToTheConnection(t *testing.T) {
	conn := &cancellingConnection{}
	model := NewModel[user]("users", conn, newTestGrammar(), &testProcessor{})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := model.NewQuery().Update(ctx, auth.SystemGrant("users.write", "acme"),
		map[string]any{"name": "Ada"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Update returned %v, want context.Canceled", err)
	}
}
