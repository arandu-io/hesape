package pipeline_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/pipeline"
)

// record is the pipe every ordering test is built from: it marks the way in and
// the way out, which is the only way to tell an onion from a queue.
func record(order *[]string, name string) pipeline.Pipe[string] {
	return func(passable string, next pipeline.Destination[string]) (string, error) {
		*order = append(*order, "in:"+name)
		out, err := next(passable)
		*order = append(*order, "out:"+name)
		return out, err
	}
}

// TestThenRunsThePipesInOrder: the first pipe given to Through is the
// outermost.
func TestThenRunsThePipesInOrder(t *testing.T) {
	var order []string

	got, err := pipeline.New[string]().
		Send("invoice").
		Through(record(&order, "first"), record(&order, "second")).
		Then(func(passable string) (string, error) {
			order = append(order, "destination:"+passable)
			return passable, nil
		})
	if err != nil {
		t.Fatalf("Then: %v", err)
	}
	if got != "invoice" {
		t.Fatalf("result = %q, want %q", got, "invoice")
	}

	want := "in:first,in:second,destination:invoice,out:second,out:first"
	if have := strings.Join(order, ","); have != want {
		t.Fatalf("order = %s, want %s", have, want)
	}
}

// TestPipesCarryTheChangedPassable: each pipe hands the next one whatever it
// passes to next, not what Send was given.
func TestPipesCarryTheChangedPassable(t *testing.T) {
	upper := pipeline.Pipe[string](func(passable string, next pipeline.Destination[string]) (string, error) {
		return next(strings.ToUpper(passable))
	})
	exclaim := pipeline.Pipe[string](func(passable string, next pipeline.Destination[string]) (string, error) {
		return next(passable + "!")
	})

	got, err := pipeline.New[string]().Send("ok").Through(upper, exclaim).ThenReturn()
	if err != nil {
		t.Fatalf("ThenReturn: %v", err)
	}
	if want := "OK!"; got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
}

// TestThenReturnAnswersWithThePassable: thenReturn() is then(fn ($p) => $p), so
// what comes back is the value the last pipe handed on.
func TestThenReturnAnswersWithThePassable(t *testing.T) {
	got, err := pipeline.New[int]().Send(41).Through(
		func(passable int, next pipeline.Destination[int]) (int, error) { return next(passable + 1) },
	).ThenReturn()
	if err != nil {
		t.Fatalf("ThenReturn: %v", err)
	}
	if got != 42 {
		t.Fatalf("result = %d, want 42", got)
	}
}

// TestThenWithoutPipesReachesTheDestination: an empty list is not a pipeline
// that does nothing, it is a pipeline that is only its destination.
func TestThenWithoutPipesReachesTheDestination(t *testing.T) {
	got, err := pipeline.New[string]().Send("bare").Then(func(passable string) (string, error) {
		return passable + ":done", nil
	})
	if err != nil {
		t.Fatalf("Then: %v", err)
	}
	if want := "bare:done"; got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
}

// TestZeroValuePipelineRuns: the zero value has no pipes and the zero passable,
// and it must not need a constructor to be sent anywhere.
func TestZeroValuePipelineRuns(t *testing.T) {
	var p pipeline.Pipeline[int]

	got, err := p.Then(func(passable int) (int, error) { return passable + 7, nil })
	if err != nil {
		t.Fatalf("Then: %v", err)
	}
	if got != 7 {
		t.Fatalf("result = %d, want 7: the zero passable is 0", got)
	}
}

// TestPipeCanStopThePipeline: a pipe that returns without calling next answers
// for the whole pipeline, which is what an authorization check does.
func TestPipeCanStopThePipeline(t *testing.T) {
	reached := false

	refuse := pipeline.Pipe[string](func(string, pipeline.Destination[string]) (string, error) {
		return "refused", nil
	})

	got, err := pipeline.New[string]().Send("order").Through(refuse).Then(func(passable string) (string, error) {
		reached = true
		return passable, nil
	})
	if err != nil {
		t.Fatalf("Then: %v", err)
	}
	if got != "refused" {
		t.Fatalf("result = %q, want %q", got, "refused")
	}
	if reached {
		t.Fatal("the destination ran: a pipe that does not call next must stop the pipeline")
	}
}

// TestErrorFromAPipeComesBack checks that a pipe's error is returned from Then
// and that nothing downstream of it ran.
func TestErrorFromAPipeComesBack(t *testing.T) {
	boom := errors.New("pipeline_test: boom")
	reached := false

	fail := pipeline.Pipe[int](func(int, pipeline.Destination[int]) (int, error) { return 0, boom })

	_, err := pipeline.New[int]().Send(1).Through(fail).Then(func(passable int) (int, error) {
		reached = true
		return passable, nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
	if reached {
		t.Fatal("the destination ran after a pipe failed")
	}
}

// TestErrorFromTheDestinationComesBack: the destination is inside the onion, so
// its failure travels back out through every pipe.
func TestErrorFromTheDestinationComesBack(t *testing.T) {
	boom := errors.New("pipeline_test: boom")

	var order []string
	_, err := pipeline.New[string]().Send("x").Through(record(&order, "only")).Then(func(string) (string, error) {
		return "", boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
	if want := "in:only,out:only"; strings.Join(order, ",") != want {
		t.Fatalf("order = %s, want %s: a pipe still unwinds when the destination fails", order, want)
	}
}

// TestThroughReplacesAndPipeAppends: through() assigns and pipe() array_pushes.
// Getting this backwards silently doubles or drops a stage.
func TestThroughReplacesAndPipeAppends(t *testing.T) {
	var order []string

	_, err := pipeline.New[string]().
		Send("x").
		Through(record(&order, "dropped")).
		Through(record(&order, "kept")).
		Pipe(record(&order, "appended")).
		ThenReturn()
	if err != nil {
		t.Fatalf("ThenReturn: %v", err)
	}

	want := "in:kept,in:appended,out:appended,out:kept"
	if have := strings.Join(order, ","); have != want {
		t.Fatalf("order = %s, want %s", have, want)
	}
}

// TestThroughWithoutPipesEmptiesTheList: through([]) is how a caller clears what
// it set, and it must not be read as "leave it alone".
func TestThroughWithoutPipesEmptiesTheList(t *testing.T) {
	var order []string

	_, err := pipeline.New[string]().Send("x").Through(record(&order, "first")).Through().ThenReturn()
	if err != nil {
		t.Fatalf("ThenReturn: %v", err)
	}
	if len(order) != 0 {
		t.Fatalf("order = %v, want nothing: Through() with no pipes empties the list", order)
	}
}

// TestPipeAppendsToAPipelineThatHasNone: pipe() on a pipeline that never had
// through() called is the whole list, not an error.
func TestPipeAppendsToAPipelineThatHasNone(t *testing.T) {
	var order []string

	if _, err := pipeline.New[string]().Send("x").Pipe(record(&order, "only")).ThenReturn(); err != nil {
		t.Fatalf("ThenReturn: %v", err)
	}
	if want := "in:only,out:only"; strings.Join(order, ",") != want {
		t.Fatalf("order = %s, want %s", order, want)
	}
}

// TestThroughCopiesTheCallersSlice: a caller that reuses its own slice -- a
// router building one list per route is the case -- must not be able to change
// a pipeline it already built.
func TestThroughCopiesTheCallersSlice(t *testing.T) {
	var order []string

	pipes := []pipeline.Pipe[string]{record(&order, "original")}
	p := pipeline.New[string]().Send("x").Through(pipes...)
	pipes[0] = record(&order, "swapped")

	if _, err := p.ThenReturn(); err != nil {
		t.Fatalf("ThenReturn: %v", err)
	}
	if want := "in:original,out:original"; strings.Join(order, ",") != want {
		t.Fatalf("order = %s, want %s", order, want)
	}
}

// TestFinallyRunsOnSuccessWithTheSentPassable checks that the callback is
// handed the value given to Send, not the value the pipes passed each other.
func TestFinallyRunsOnSuccessWithTheSentPassable(t *testing.T) {
	var seen string
	calls := 0

	got, err := pipeline.New[string]().
		Send("sent").
		Through(func(passable string, next pipeline.Destination[string]) (string, error) {
			return next(passable + ":changed")
		}).
		Finally(func(passable string) {
			seen = passable
			calls++
		}).
		ThenReturn()
	if err != nil {
		t.Fatalf("ThenReturn: %v", err)
	}
	if got != "sent:changed" {
		t.Fatalf("result = %q, want %q", got, "sent:changed")
	}
	if seen != "sent" {
		t.Fatalf("Finally saw %q, want %q: it is given the passable as Send left it", seen, "sent")
	}
	if calls != 1 {
		t.Fatalf("Finally ran %d times, want once", calls)
	}
}

// TestFinallyRunsWhenAPipeFails: it is a finally block, not a success hook.
func TestFinallyRunsWhenAPipeFails(t *testing.T) {
	boom := errors.New("pipeline_test: boom")
	ran := false

	_, err := pipeline.New[int]().
		Send(1).
		Through(func(int, pipeline.Destination[int]) (int, error) { return 0, boom }).
		Finally(func(int) { ran = true }).
		ThenReturn()
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
	if !ran {
		t.Fatal("Finally did not run after a failure")
	}
}

// TestFinallyRunsWhenAPipePanics checks that the deferred callback runs while
// the panic is on its way out, and that the panic keeps going.
func TestFinallyRunsWhenAPipePanics(t *testing.T) {
	ran := false

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("the panic did not travel out of Then")
			}
		}()

		_, _ = pipeline.New[int]().
			Send(1).
			Through(func(int, pipeline.Destination[int]) (int, error) { panic("held wrong") }).
			Finally(func(int) { ran = true }).
			ThenReturn()
	}()

	if !ran {
		t.Fatal("Finally did not run while a panic was unwinding")
	}
}

// TestFinallyKeepsTheSecondCallback: assigning the property twice keeps the
// second, and two callbacks would be a set nobody declared.
func TestFinallyKeepsTheSecondCallback(t *testing.T) {
	var ran []string

	if _, err := pipeline.New[int]().
		Send(1).
		Finally(func(int) { ran = append(ran, "first") }).
		Finally(func(int) { ran = append(ran, "second") }).
		ThenReturn(); err != nil {
		t.Fatalf("ThenReturn: %v", err)
	}

	if len(ran) != 1 || ran[0] != "second" {
		t.Fatalf("callbacks ran = %v, want [second]", ran)
	}
}

// TestThenWithoutADestinationFails checks that a nil destination fails before
// anything else happens -- before the pipes and before the Finally callback.
func TestThenWithoutADestinationFails(t *testing.T) {
	ran := false
	reached := false

	_, err := pipeline.New[int]().
		Send(1).
		Through(func(passable int, next pipeline.Destination[int]) (int, error) {
			reached = true
			return next(passable)
		}).
		Finally(func(int) { ran = true }).
		Then(nil)
	if err == nil {
		t.Fatal("Then(nil) returned no error")
	}
	if !strings.Contains(err.Error(), "pipeline: ") {
		t.Fatalf("error = %v, want it to name the package", err)
	}
	if reached {
		t.Fatal("a pipe ran without a destination")
	}
	if ran {
		t.Fatal("Finally ran for a call that never started")
	}
}

// TestNilPipeFailsWhenThePipelineReachesIt.
func TestNilPipeFailsWhenThePipelineReachesIt(t *testing.T) {
	reached := false

	_, err := pipeline.New[int]().Send(1).Through(nil).Then(func(passable int) (int, error) {
		reached = true
		return passable, nil
	})
	if err == nil {
		t.Fatal("a nil pipe returned no error")
	}
	if !strings.Contains(err.Error(), "pipe 0") {
		t.Fatalf("error = %v, want it to say which pipe is nil", err)
	}
	if reached {
		t.Fatal("the destination ran past a nil pipe")
	}
}

// TestNilPipeIsHarmlessWhenNobodyReachesIt checks that a stage an earlier pipe
// never calls is never evaluated, so a nil one behind a stop is not an error.
func TestNilPipeIsHarmlessWhenNobodyReachesIt(t *testing.T) {
	stop := pipeline.Pipe[int](func(int, pipeline.Destination[int]) (int, error) { return 9, nil })

	got, err := pipeline.New[int]().Send(1).Through(stop, nil).ThenReturn()
	if err != nil {
		t.Fatalf("ThenReturn: %v", err)
	}
	if got != 9 {
		t.Fatalf("result = %d, want 9", got)
	}
}

// TestPipelineCanBeSentTwice: the builder keeps its pipes, and running it again
// runs them again against whatever Send left last.
func TestPipelineCanBeSentTwice(t *testing.T) {
	p := pipeline.New[int]().Through(func(passable int, next pipeline.Destination[int]) (int, error) {
		return next(passable * 2)
	})

	first, err := p.Send(2).ThenReturn()
	if err != nil {
		t.Fatalf("ThenReturn: %v", err)
	}
	second, err := p.Send(5).ThenReturn()
	if err != nil {
		t.Fatalf("ThenReturn: %v", err)
	}
	if first != 4 || second != 10 {
		t.Fatalf("results = %d and %d, want 4 and 10", first, second)
	}
}

func TestWhenAddsAPipeOnlyWhenTheConditionHolds(t *testing.T) {
	add := func(suffix string) pipeline.Pipe[string] {
		return func(passable string, next pipeline.Destination[string]) (string, error) {
			return next(passable + suffix)
		}
	}
	build := func(condition bool) (string, error) {
		return pipeline.New[string]().
			Send("a").
			Through(add("b")).
			When(condition, func(p *pipeline.Pipeline[string]) *pipeline.Pipeline[string] {
				return p.Pipe(add("c"))
			}, nil).
			ThenReturn()
	}

	got, err := build(true)
	if err != nil {
		t.Fatalf("When(true): %v", err)
	}
	if got != "abc" {
		t.Fatalf("When(true) = %q, want abc", got)
	}

	got, err = build(false)
	if err != nil {
		t.Fatalf("When(false): %v", err)
	}
	if got != "ab" {
		t.Fatalf("When(false) = %q, want ab", got)
	}
}

func TestWhenTakesTheDefaultBranch(t *testing.T) {
	mark := func(suffix string) func(*pipeline.Pipeline[string]) *pipeline.Pipeline[string] {
		return func(p *pipeline.Pipeline[string]) *pipeline.Pipeline[string] {
			return p.Pipe(func(passable string, next pipeline.Destination[string]) (string, error) {
				return next(passable + suffix)
			})
		}
	}

	// When runs the second callback when the condition is false, and Unless is
	// When with the condition negated.
	got, err := pipeline.New[string]().Send("a").When(false, mark("x"), mark("y")).ThenReturn()
	if err != nil {
		t.Fatalf("When: %v", err)
	}
	if got != "ay" {
		t.Fatalf("When(false, x, y) = %q, want ay", got)
	}

	got, err = pipeline.New[string]().Send("a").Unless(true, mark("x"), mark("y")).ThenReturn()
	if err != nil {
		t.Fatalf("Unless: %v", err)
	}
	if got != "ay" {
		t.Fatalf("Unless(true, x, y) = %q, want ay", got)
	}

	got, err = pipeline.New[string]().Send("a").Unless(false, mark("x"), mark("y")).ThenReturn()
	if err != nil {
		t.Fatalf("Unless: %v", err)
	}
	if got != "ax" {
		t.Fatalf("Unless(false, x, y) = %q, want ax", got)
	}
}

func TestWhenWithNoBranchChangesNothing(t *testing.T) {
	// Both branches are optional: a nil callback for the branch taken leaves
	// the pipeline unchanged rather than failing.
	got, err := pipeline.New[string]().Send("a").When(true, nil, nil).Unless(true, nil, nil).ThenReturn()
	if err != nil {
		t.Fatalf("When: %v", err)
	}
	if got != "a" {
		t.Fatalf("= %q, want a", got)
	}
}
