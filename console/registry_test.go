package console_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/console"
)

// newRegistry is the shape every test here starts from: a registry over two
// buffers and a script of answers.
func newRegistry(t *testing.T, input string) (*console.Registry, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	return console.New(&out, &errOut, strings.NewReader(input)), &out, &errOut
}

func TestHandleRunsTheCommandAndHandsItTheArguments(t *testing.T) {
	var got []string
	r, out, _ := newRegistry(t, "")
	r.Add(console.Command{
		Name:        "invoice:close",
		Description: "close the open invoices of the period",
		Run: func(_ context.Context, o *console.IO) error {
			got = o.Args()
			o.Line("closed")
			return nil
		},
	})

	if err := r.Handle(context.Background(), []string{"invoice:close", "--force", "2026-08"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(got) != 2 || got[0] != "--force" || got[1] != "2026-08" {
		t.Errorf("the command got %v, want the two arguments that followed its name", got)
	}
	if out.String() != "closed\n" {
		t.Errorf("output = %q", out.String())
	}
}

// TestHandleOnAnUnknownCommand: an error that only says the command is unknown
// costs a search; this one ends it.
func TestHandleOnAnUnknownCommand(t *testing.T) {
	r, _, _ := newRegistry(t, "")
	r.Add(console.Command{Name: "migrate", Description: "apply the migrations", Run: nothing})

	err := r.Handle(context.Background(), []string{"migrat"})
	if err == nil {
		t.Fatal("an unknown command ran")
	}
	if got := console.ExitCode(err); got != 1 {
		t.Errorf("ExitCode = %d, want 1", got)
	}
	if !strings.Contains(err.Error(), "migrate") {
		t.Errorf("the error does not list what was available: %s", err)
	}
}

func TestHandleWithNothingPrintsTheListing(t *testing.T) {
	r, out, _ := newRegistry(t, "")
	r.Add(console.Command{Name: "migrate", Description: "apply the migrations", Run: nothing})

	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}} {
		out.Reset()
		if err := r.Handle(context.Background(), args); err != nil {
			t.Fatalf("Handle(%v): %v", args, err)
		}
		if !strings.Contains(out.String(), "apply the migrations") {
			t.Errorf("Handle(%v) printed %q, want the listing", args, out.String())
		}
	}
}

// TestHiddenStaysOutOfTheListingAndStillRuns: it is for the command another
// program calls, which would only be noise to a person reading the list.
func TestHiddenStaysOutOfTheListingAndStillRuns(t *testing.T) {
	ran := false
	r, _, _ := newRegistry(t, "")
	r.Add(
		console.Command{Name: "migrate", Description: "apply the migrations", Run: nothing},
		console.Command{Name: "internal:probe", Description: "answer a health check", Hidden: true,
			Run: func(context.Context, *console.IO) error { ran = true; return nil }},
	)

	if names := r.Names(); len(names) != 1 || names[0] != "migrate" {
		t.Errorf("Names = %v, want only the visible one", names)
	}
	if strings.Contains(r.Help(), "internal:probe") {
		t.Errorf("the hidden command is in the listing:\n%s", r.Help())
	}

	if err := r.Handle(context.Background(), []string{"internal:probe"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !ran {
		t.Error("the hidden command did not run when it was asked for by name")
	}
}

// TestHelpIsSortedByName, so the group prefix does the grouping and adding a
// command cannot move an unrelated one.
func TestHelpIsSortedByName(t *testing.T) {
	r, _, _ := newRegistry(t, "")
	r.Add(
		console.Command{Name: "queue:work", Description: "drain a queue", Run: nothing},
		console.Command{Name: "make:model", Description: "generate an entity", Run: nothing},
		console.Command{Name: "make:job", Description: "generate a job", Run: nothing},
	)

	help := r.Help()
	if i, j := strings.Index(help, "make:job"), strings.Index(help, "make:model"); i > j {
		t.Errorf("make:job comes after make:model:\n%s", help)
	}
	if i, j := strings.Index(help, "make:model"), strings.Index(help, "queue:work"); i > j {
		t.Errorf("make:model comes after queue:work:\n%s", help)
	}
}

func TestHelpWithNothingRegistered(t *testing.T) {
	r, _, _ := newRegistry(t, "")
	if got, want := r.Help(), "no commands registered\n"; got != want {
		t.Errorf("Help = %q, want %q", got, want)
	}
}

// TestCallRunsOneCommandFromInsideAnother, which is how migrate:fresh is
// written without either half being invisible to the listing.
func TestCall(t *testing.T) {
	r, out, _ := newRegistry(t, "")
	r.Add(
		console.Command{Name: "migrate", Description: "apply the migrations", Run: func(_ context.Context, o *console.IO) error {
			o.Line("migrated %s", strings.Join(o.Args(), " "))
			return nil
		}},
		console.Command{Name: "migrate:fresh", Description: "drop everything and migrate again", Run: func(ctx context.Context, o *console.IO) error {
			o.Line("dropped")
			return r.Call(ctx, "migrate", "--step")
		}},
	)

	if err := r.Handle(context.Background(), []string{"migrate:fresh"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got, want := out.String(), "dropped\nmigrated --step\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestCallSilentKeepsTheErrorAndDropsTheChatter.
func TestCallSilent(t *testing.T) {
	broken := errors.New("the store is down")
	r, out, errOut := newRegistry(t, "")
	r.Add(console.Command{Name: "cache:clear", Description: "empty the cache", Run: func(_ context.Context, o *console.IO) error {
		o.Line("clearing")
		o.Error("could not")
		return broken
	}})

	if err := r.CallSilent(context.Background(), "cache:clear"); !errors.Is(err, broken) {
		t.Fatalf("CallSilent returned %v, want the error unchanged", err)
	}
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Errorf("CallSilent wrote %q and %q", out.String(), errOut.String())
	}
}

func TestCallOnAnUnknownCommand(t *testing.T) {
	r, _, _ := newRegistry(t, "")
	if err := r.Call(context.Background(), "nope"); err == nil {
		t.Error("Call ran a command that is not registered")
	}
	if err := r.CallSilent(context.Background(), "nope"); err == nil {
		t.Error("CallSilent ran a command that is not registered")
	}
}

func TestObserveSeesEveryRun(t *testing.T) {
	var runs []console.Run
	broken := errors.New("no connection")

	r, _, _ := newRegistry(t, "")
	r.Observe(func(run console.Run) { runs = append(runs, run) })
	r.Add(console.Command{Name: "migrate", Description: "apply the migrations", Run: func(context.Context, *console.IO) error {
		return broken
	}})

	if err := r.Handle(context.Background(), []string{"migrate", "--step"}); !errors.Is(err, broken) {
		t.Fatalf("Handle returned %v, want the error unchanged", err)
	}
	if len(runs) != 1 {
		t.Fatalf("the observer saw %d runs, want 1", len(runs))
	}
	if runs[0].Name != "migrate" || len(runs[0].Args) != 1 || !errors.Is(runs[0].Err, broken) {
		t.Errorf("the observer saw %+v", runs[0])
	}
}

// TestIsolatedWithoutAnIssuer is refused rather than run unprotected: a command
// that says it must not overlap and then does is worse than one that does not
// start.
func TestIsolatedWithoutAnIssuer(t *testing.T) {
	ran := false
	r, _, _ := newRegistry(t, "")
	r.Add(console.Command{Name: "invoice:close", Description: "close the invoices", Isolated: "invoice:close",
		Run: func(context.Context, *console.IO) error { ran = true; return nil }})

	err := r.Handle(context.Background(), []string{"invoice:close"})
	if err == nil {
		t.Fatal("an isolated command ran with no lock issuer wired")
	}
	if ran {
		t.Error("the command ran anyway")
	}
	if !strings.Contains(err.Error(), "WithLocks") {
		t.Errorf("the error does not say how to fix it: %s", err)
	}
}

func TestIsolatedTakesTheLock(t *testing.T) {
	locks := cache.NewLocks(cache.NewArrayStore())
	held := false

	r, _, _ := newRegistry(t, "")
	r.WithLocks(locks, time.Minute)
	r.Add(console.Command{Name: "invoice:close", Description: "close the invoices", Isolated: "invoice:close",
		Run: func(ctx context.Context, _ *console.IO) error {
			// While the command holds it, nobody else can.
			held = errors.Is(locks.Lock("invoice:close", time.Minute).Acquire(ctx), cache.ErrLocked)
			return nil
		}})

	if err := r.Handle(context.Background(), []string{"invoice:close"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !held {
		t.Error("the command ran without holding its lock")
	}

	// And it gave the lock back.
	if err := locks.Lock("invoice:close", time.Minute).Acquire(context.Background()); err != nil {
		t.Errorf("the lock was not released: %v", err)
	}
}

// TestIsolatedWhenSomebodyElseHoldsIt: the work is being done, so the status
// stays zero. A cron entry that paged every minute is what this prevents.
func TestIsolatedWhenSomebodyElseHoldsIt(t *testing.T) {
	locks := cache.NewLocks(cache.NewArrayStore())
	if err := locks.Lock("invoice:close", time.Minute).Acquire(context.Background()); err != nil {
		t.Fatalf("taking the lock first: %v", err)
	}

	ran := false
	var seen []console.Run

	r, out, _ := newRegistry(t, "")
	r.WithLocks(locks, time.Minute).Observe(func(run console.Run) { seen = append(seen, run) })
	r.Add(console.Command{Name: "invoice:close", Description: "close the invoices", Isolated: "invoice:close",
		Run: func(context.Context, *console.IO) error { ran = true; return nil }})

	if err := r.Handle(context.Background(), []string{"invoice:close"}); err != nil {
		t.Fatalf("Handle = %v, want nil: another process doing the work is the answer, not a failure", err)
	}
	if ran {
		t.Error("the command ran while another process held its lock")
	}
	if !strings.Contains(out.String(), "already running") {
		t.Errorf("nothing said why it did not run: %q", out.String())
	}
	if len(seen) != 1 || !errors.Is(seen[0].Err, cache.ErrLocked) {
		t.Errorf("the observer saw %+v, want the lock reported", seen)
	}
}

func TestAddRefusesWhatCannotWork(t *testing.T) {
	for name, add := range map[string]func(*console.Registry){
		"no name": func(r *console.Registry) { r.Add(console.Command{Run: nothing}) },
		"no Run":  func(r *console.Registry) { r.Add(console.Command{Name: "migrate"}) },
		"twice": func(r *console.Registry) {
			r.Add(console.Command{Name: "migrate", Run: nothing})
			r.Add(console.Command{Name: "migrate", Run: nothing})
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Add accepted a command that could never work")
				}
			}()
			r, _, _ := newRegistry(t, "")
			add(r)
		})
	}
}

func nothing(context.Context, *console.IO) error { return nil }
