package query_test

import (
	"testing"

	"github.com/arandu-io/hesape/database/query"
)

// newBuilder builds a Builder with no connection, grammar or processor: the
// conditional methods touch none of the three, and a nil there keeps the test
// from depending on a grammar that has not been written yet.
func newBuilder() *query.Builder {
	return query.NewBuilder(nil, nil, nil).From("posts")
}

func TestWhenAppliesTheCallbackOnATrueCondition(t *testing.T) {
	b := newBuilder().
		When(true, func(q *query.Builder) { q.Where("published", true) }, nil).
		OrderBy("id")

	if len(b.Wheres) != 1 {
		t.Fatalf("wheres = %d, want 1", len(b.Wheres))
	}
	if b.Wheres[0].Column != "published" {
		t.Errorf("column = %v, want published", b.Wheres[0].Column)
	}
	if bindings := b.GetBindings(); len(bindings) != 1 || bindings[0] != true {
		t.Errorf("bindings = %v, want [true]", bindings)
	}
	if len(b.Orders) != 1 {
		t.Errorf("orders = %d, want 1: the chain has to continue past When", len(b.Orders))
	}
}

func TestWhenSkipsTheCallbackOnAFalseCondition(t *testing.T) {
	b := newBuilder().When(false, func(q *query.Builder) { q.Where("published", true) }, nil)

	if len(b.Wheres) != 0 {
		t.Errorf("wheres = %d, want 0", len(b.Wheres))
	}
	if bindings := b.GetBindings(); len(bindings) != 0 {
		t.Errorf("bindings = %v, want none: a skipped callback binds nothing", bindings)
	}
}

func TestWhenRunsTheDefaultOnAFalseCondition(t *testing.T) {
	b := newBuilder().When(
		false,
		func(q *query.Builder) { q.Where("published", true) },
		func(q *query.Builder) { q.Where("draft", true) },
	)

	if len(b.Wheres) != 1 {
		t.Fatalf("wheres = %d, want 1", len(b.Wheres))
	}
	if b.Wheres[0].Column != "draft" {
		t.Errorf("column = %v, want draft", b.Wheres[0].Column)
	}
}

func TestWhenLeavesTheBuilderAloneWithNilCallbacks(t *testing.T) {
	for _, condition := range []bool{true, false} {
		b := newBuilder().When(condition, nil, nil)
		if len(b.Wheres) != 0 {
			t.Errorf("condition %v: wheres = %d, want 0", condition, len(b.Wheres))
		}
	}
}

func TestWhenReturnsTheSameBuilder(t *testing.T) {
	b := newBuilder()
	if got := b.When(true, func(*query.Builder) {}, nil); got != b {
		t.Error("When returned a different builder; the callback mutates in place")
	}
	if got := b.Unless(true, func(*query.Builder) {}, nil); got != b {
		t.Error("Unless returned a different builder; the callback mutates in place")
	}
}

func TestUnlessNegatesTheCondition(t *testing.T) {
	applied := newBuilder().Unless(false, func(q *query.Builder) { q.Where("published", true) }, nil)
	if len(applied.Wheres) != 1 {
		t.Errorf("unless(false): wheres = %d, want 1", len(applied.Wheres))
	}

	skipped := newBuilder().Unless(true, func(q *query.Builder) { q.Where("published", true) }, nil)
	if len(skipped.Wheres) != 0 {
		t.Errorf("unless(true): wheres = %d, want 0", len(skipped.Wheres))
	}
}

func TestUnlessRunsTheDefaultOnATrueCondition(t *testing.T) {
	b := newBuilder().Unless(
		true,
		func(q *query.Builder) { q.Where("published", true) },
		func(q *query.Builder) { q.Where("draft", true) },
	)

	if len(b.Wheres) != 1 {
		t.Fatalf("wheres = %d, want 1", len(b.Wheres))
	}
	if b.Wheres[0].Column != "draft" {
		t.Errorf("column = %v, want draft", b.Wheres[0].Column)
	}
}

func TestWhenNests(t *testing.T) {
	b := newBuilder().When(true, func(q *query.Builder) {
		q.When(true, func(inner *query.Builder) { inner.Where("author", 1) }, nil)
	}, nil)

	if len(b.Wheres) != 1 {
		t.Fatalf("wheres = %d, want 1", len(b.Wheres))
	}
	if bindings := b.GetBindings(); len(bindings) != 1 || bindings[0] != 1 {
		t.Errorf("bindings = %v, want [1]", bindings)
	}
}
