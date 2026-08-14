package bus_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/bus"
)

// advance plays one link: read the envelope, report success or failure, and
// return what got pushed as a result.
func advance(t *testing.T, queue *recorder, j pushed, cause error) row {
	t.Helper()
	var r row
	m, err := bus.Batched(j.Payload, &r)
	if err != nil {
		t.Fatalf("Batched: %v", err)
	}
	if err := bus.Handled(context.Background(), grant(), nil, queue, m, cause); err != nil {
		t.Fatalf("Handled: %v", err)
	}
	return r
}

func TestChainRunsOneLinkAtATime(t *testing.T) {
	t.Parallel()

	queue := &recorder{}
	err := bus.NewChain().
		Add("export.rows", row{N: 1}).
		Add("export.compress", row{N: 2}).
		Add("export.email", row{N: 3}).
		OnQueue("exports").
		Dispatch(context.Background(), grant(), queue)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// Only the first link is queued. That is the property: there is no moment
	// at which two links are in flight.
	if got := queue.names(); len(got) != 1 || got[0] != "export.rows" {
		t.Fatalf("queued %v, want only export.rows", got)
	}

	if got := advance(t, queue, queue.all()[0], nil); got.N != 1 {
		t.Errorf("the first link carries n=%d, want 1", got.N)
	}
	if got := queue.names(); len(got) != 2 || got[1] != "export.compress" {
		t.Fatalf("queued %v, want export.compress second", got)
	}

	if got := advance(t, queue, queue.all()[1], nil); got.N != 2 {
		t.Errorf("the second link carries n=%d, want 2", got.N)
	}
	if got := advance(t, queue, queue.all()[2], nil); got.N != 3 {
		t.Errorf("the third link carries n=%d, want 3", got.N)
	}

	// The last link ends the chain and pushes nothing.
	if got := queue.names(); len(got) != 3 {
		t.Fatalf("queued %v, want three links and nothing after", got)
	}
	for i, j := range queue.all() {
		if j.Queue != "exports" {
			t.Errorf("link %d landed on queue %q, want exports", i, j.Queue)
		}
	}
}

func TestChainStopsAtTheFirstFailure(t *testing.T) {
	t.Parallel()

	queue := &recorder{}
	err := bus.NewChain().
		Add("export.rows", row{N: 1}).
		Add("export.compress", row{N: 2}).
		Catch("export.failed", row{N: 9}).
		OnQueue("exports").
		Dispatch(context.Background(), grant(), queue)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	advance(t, queue, queue.all()[0], errors.New("the rows would not export"))

	got := queue.names()
	if len(got) != 2 || got[1] != "export.failed" {
		t.Fatalf("queued %v, want the chain to stop and report", got)
	}
	// Compressing a file that was never written is what a chain exists to
	// prevent.
	for _, name := range got {
		if name == "export.compress" {
			t.Fatal("the second link ran after the first failed")
		}
	}

	var r row
	m, err := bus.Batched(queue.all()[1].Payload, &r)
	if err != nil {
		t.Fatalf("Batched: %v", err)
	}
	if len(m.Chained) != 0 {
		t.Error("the Catch job carries the rest of the chain")
	}
	if r.N != 9 {
		t.Errorf("the Catch job carries n=%d, want 9", r.N)
	}
	if queue.all()[1].Queue != "exports" {
		t.Errorf("the Catch job landed on queue %q, want exports", queue.all()[1].Queue)
	}
}

func TestChainWithoutCatchJustStops(t *testing.T) {
	t.Parallel()

	queue := &recorder{}
	err := bus.NewChain().
		Add("export.rows", row{N: 1}).
		Add("export.compress", row{N: 2}).
		Dispatch(context.Background(), grant(), queue)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	advance(t, queue, queue.all()[0], errors.New("nope"))
	if got := queue.names(); len(got) != 1 {
		t.Fatalf("queued %v, want nothing after the failure", got)
	}
}

// TestHandledSaysWhatIsMissing: a batch member reported with no store is a
// wiring mistake, and the message has to name it rather than drop the report.
func TestHandledSaysWhatIsMissing(t *testing.T) {
	t.Parallel()

	_, _, queue := dispatch(t, bus.NewBatch("import").Add("invoice.import", row{N: 1}))
	m, err := bus.Batched(queue.all()[0].Payload, nil)
	if err != nil {
		t.Fatalf("Batched: %v", err)
	}

	if err := bus.Handled(context.Background(), grant(), nil, queue, m, nil); err == nil {
		t.Error("a batch member was reported with no store and nobody complained")
	}
	if err := bus.Handled(context.Background(), grant(), bus.NewMemory(), nil, m, nil); err == nil {
		t.Error("a batch member was reported with no queue and nobody complained")
	}
}

// TestDispatchChainQueuesTheFirstLinkAndCarriesTheRest covers Bus::dispatchChain:
// the jobs handed to it become a chain and that chain is dispatched in the same
// call, so the first link is queued and the others travel inside it.
func TestDispatchChainQueuesTheFirstLinkAndCarriesTheRest(t *testing.T) {
	t.Parallel()

	queue := &recorder{}
	d := bus.NewDispatcher(queue, nil)

	err := d.DispatchChain(context.Background(), grant(),
		job(t, "export.rows", row{N: 1}),
		job(t, "export.compress", row{N: 2}),
		job(t, "export.email", row{N: 3}),
	)
	if err != nil {
		t.Fatalf("DispatchChain: %v", err)
	}

	if got := queue.names(); len(got) != 1 || got[0] != "export.rows" {
		t.Fatalf("queued %v, want only export.rows", got)
	}

	var first row
	m, err := bus.Batched(queue.all()[0].Payload, &first)
	if err != nil {
		t.Fatalf("Batched: %v", err)
	}
	if first.N != 1 {
		t.Errorf("the queued link carries n=%d, want 1", first.N)
	}
	if len(m.Chained) != 2 {
		t.Fatalf("the queued link carries %d remaining links, want 2", len(m.Chained))
	}
	if m.Chained[0].Name != "export.compress" || m.Chained[1].Name != "export.email" {
		t.Errorf("the remaining links are %+v, want compress then email", m.Chained)
	}

	// The chain runs to its end from the envelope alone, which is what makes
	// dispatching it in one call the same thing as building it and dispatching it.
	if got := advance(t, queue, queue.all()[0], nil); got.N != 1 {
		t.Errorf("the first link replayed as n=%d, want 1", got.N)
	}
	if got := queue.names(); len(got) != 2 || got[1] != "export.compress" {
		t.Fatalf("queued %v, want export.compress second", got)
	}
}

// TestDispatchChainRefusesWhatAChainRefuses: DispatchChain is Chain.Dispatch, so
// the empty chain, the missing tenant and the dispatcher with no queue are
// reported rather than half-run.
func TestDispatchChainRefusesWhatAChainRefuses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	queue := &recorder{}

	if err := bus.NewDispatcher(queue, nil).DispatchChain(ctx, grant()); !errors.Is(err, bus.ErrEmptyChain) {
		t.Errorf("err = %v, want ErrEmptyChain", err)
	}

	err := bus.NewDispatcher(queue, nil).DispatchChain(ctx, auth.Grant{}, job(t, "export.rows", row{N: 1}))
	if !errors.Is(err, bus.ErrNoTenant) {
		t.Errorf("err = %v, want ErrNoTenant", err)
	}

	// A dispatcher with no queue cannot push a chain: running the first link in
	// process would take the links it carries with it.
	if err := bus.NewDispatcher(nil, nil).DispatchChain(ctx, grant(), job(t, "export.rows", row{N: 1})); err == nil {
		t.Error("a chain was dispatched with no queue and nobody complained")
	}
}

func TestChainRefusesTheEmptyAndTheUngranted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if err := bus.NewChain().Dispatch(ctx, grant(), &recorder{}); !errors.Is(err, bus.ErrEmptyChain) {
		t.Errorf("err = %v, want ErrEmptyChain", err)
	}

	err := bus.NewChain().Add("export.rows", row{N: 1}).Dispatch(ctx, auth.Grant{}, &recorder{})
	if !errors.Is(err, bus.ErrNoTenant) {
		t.Errorf("err = %v, want ErrNoTenant", err)
	}

	err = bus.NewChain().Add("", row{N: 1}).Dispatch(ctx, grant(), &recorder{})
	if !errors.Is(err, bus.ErrNoName) {
		t.Errorf("err = %v, want ErrNoName", err)
	}
}
