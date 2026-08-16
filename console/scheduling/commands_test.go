package scheduling_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/console/scheduling"
)

// fakeClock is the runner's clock and its sleep, wired to each other: sleeping
// is how a test moves through the minute the repeat loop lives in.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(d time.Duration) { c.now = c.now.Add(d) }

// TestASubMinuteEventRepeatsWithinTheMinute: EveryFifteenSeconds recorded
// repeatSeconds and nothing ever read it -- ShouldRepeatNow had no caller at
// all -- so an event declared to run four times a minute ran once.
func TestASubMinuteEventRepeatsWithinTheMinute(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)

	ran := 0
	s.Call(func(context.Context, auth.Grant) error { ran++; return nil }).
		Name("poll the queue").
		EveryFifteenSeconds()

	at := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: at}

	runner := scheduling.NewRunner(s)
	runner.Now = clock.Now
	runner.Sleep = clock.Sleep

	if got := runner.Run(t.Context(), at); got != 4 {
		t.Fatalf("the runner reported %d runs in the minute, want 4", got)
	}
	if ran != 4 {
		t.Fatalf("the event ran %d times in the minute, want 4: 15 seconds is four times a minute", ran)
	}
}

// TestAnEventWithNoRepeatRunsOnce: the repeat loop must not hold the minute open
// for a schedule that never asked to repeat.
func TestAnEventWithNoRepeatRunsOnce(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)

	ran := 0
	s.Call(func(context.Context, auth.Grant) error { ran++; return nil }).
		Name("hourly report").
		EveryMinute()

	at := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: at}

	runner := scheduling.NewRunner(s)
	runner.Now = clock.Now
	runner.Sleep = clock.Sleep
	runner.Run(t.Context(), at)

	if ran != 1 {
		t.Fatalf("the event ran %d times, want 1", ran)
	}
	if !clock.now.Equal(at) {
		t.Fatalf("the runner slept until %s: a schedule with nothing to repeat holds nothing open", clock.now)
	}
}

// TestTheRepeatLoopStopsWhenTheContextIsCancelled: the loop owns the rest of the
// minute, so a stopped process has to be able to take it back.
func TestTheRepeatLoopStopsWhenTheContextIsCancelled(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)

	ctx, cancel := context.WithCancel(t.Context())
	s.Call(func(context.Context, auth.Grant) error {
		cancel()
		return nil
	}).Name("poll the queue").EveryFifteenSeconds()

	at := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: at}

	runner := scheduling.NewRunner(s)
	runner.Now = clock.Now
	runner.Sleep = clock.Sleep

	if got := runner.Run(ctx, at); got != 1 {
		t.Fatalf("the runner reported %d runs, want the one that cancelled", got)
	}
}

// fakeEventMutex is an overlap lock in memory.
type fakeEventMutex struct{ held bool }

func (m *fakeEventMutex) Create(context.Context, *scheduling.Event) (bool, error) {
	if m.held {
		return false, nil
	}
	m.held = true
	return true, nil
}

func (m *fakeEventMutex) Exists(context.Context, *scheduling.Event) (bool, error) {
	return m.held, nil
}

func (m *fakeEventMutex) Forget(context.Context, *scheduling.Event) error {
	m.held = false
	return nil
}

// run builds a console application over the commands and runs one line of it.
func run(t *testing.T, commands scheduling.Commands, args ...string) (string, error) {
	t.Helper()

	var out, errOut bytes.Buffer
	app := console.NewApplication(&out, &errOut, strings.NewReader(""))
	for _, command := range commands.All() {
		app.Add(command)
	}
	err := app.Handle(t.Context(), args)
	return out.String() + errOut.String(), err
}

// TestScheduleTestRunsTheEventInTheForeground: the command ran the event as
// it was declared, so an event that says RunInBackground was started and
// walked away from -- and Event.Run returns before Finish for a background
// event, which left the overlap mutex held until it expired a day later.
func TestScheduleTestRunsTheEventInTheForeground(t *testing.T) {
	mutex := &fakeEventMutex{}
	s := scheduling.NewSchedule(mutex, nil, nil)
	s.Exec("true").Name("nightly").EveryMinute().WithoutOverlapping().RunInBackground()

	if _, err := run(t, scheduling.Commands{Schedule: s}, "schedule:test", "--name=nightly"); err != nil {
		t.Fatalf("schedule:test: %v", err)
	}

	if s.Events()[0].RunsInBackground() {
		t.Error("schedule:test left the event in the background")
	}
	if mutex.held {
		t.Error("the overlap mutex is still held: the event was backgrounded and nothing ever finished it")
	}
}

// fakeInterrupter records what schedule:interrupt asked for.
type fakeInterrupter struct {
	ttl  time.Duration
	mark bool
}

func (i *fakeInterrupter) Interrupt(_ context.Context, ttl time.Duration) error {
	i.ttl = ttl
	i.mark = true
	return nil
}

func (i *fakeInterrupter) Interrupted(context.Context) (bool, error) {
	mark := i.mark
	i.mark = false
	return mark, nil
}

// TestScheduleInterruptLastsUntilTheEndOfTheMinute: the mark was left for an
// hour, so a worker restarted at any point in the next hour was stopped by
// an interrupt meant for the process before it -- which is the opposite of
// what the command's own comment claimed.
func TestScheduleInterruptLastsUntilTheEndOfTheMinute(t *testing.T) {
	interrupter := &fakeInterrupter{}
	commands := scheduling.Commands{
		Schedule:    scheduling.NewSchedule(nil, nil, nil),
		Interrupter: interrupter,
		Now:         func() time.Time { return time.Date(2026, 8, 11, 3, 0, 20, 0, time.UTC) },
	}

	if _, err := run(t, commands, "schedule:interrupt"); err != nil {
		t.Fatalf("schedule:interrupt: %v", err)
	}

	if interrupter.ttl != 40*time.Second {
		t.Fatalf("the mark lives for %s, want the 40 seconds left in the minute", interrupter.ttl)
	}
}

// TestScheduleFinishFinishesEveryEventWithThatName: the command returned at
// the first match, so a schedule that declares the same command twice left
// the second one holding its mutex and never ran its after callbacks.
func TestScheduleFinishFinishesEveryEventWithThatName(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)

	finished := 0
	for range 2 {
		s.Exec("app:work").Daily().Then(func(context.Context) error {
			finished++
			return nil
		})
	}

	id := s.Events()[0].MutexName()
	if s.Events()[1].MutexName() != id {
		t.Fatalf("the two events do not share a mutex name, and the test proves nothing")
	}

	if _, err := run(t, scheduling.Commands{Schedule: s}, "schedule:finish", id, "0"); err != nil {
		t.Fatalf("schedule:finish: %v", err)
	}

	if finished != 2 {
		t.Fatalf("%d events were finished, want both of them", finished)
	}
}

// TestScheduleFinishWithAnUnknownIdSucceeds: it is appended to a backgrounded
// command line, so it runs in a process that has already lost the run it is
// reporting on. Exiting 1 for an id the schedule no longer has turns a
// deploy that changed the schedule into a failed background job.
func TestScheduleFinishWithAnUnknownIdSucceeds(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)
	s.Exec("app:work").Daily()

	output, err := run(t, scheduling.Commands{Schedule: s}, "schedule:finish", "framework/schedule-gone", "0")
	if err != nil {
		t.Fatalf("schedule:finish on an unknown id returned %v, want it to succeed", err)
	}
	if !strings.Contains(output, "framework/schedule-gone") {
		t.Errorf("it said nothing about the id it could not find: %q", output)
	}
}

// TestDaysOfMonthWithNoDaysIsRefused: it spliced "0" into the day-of-month
// field, and "0 0 0 * *" is not a cron expression any parser accepts -- the
// schedule refused it at registration and the application did not start. The
// mistake is the empty call, and this is where it can still be named.
func TestDaysOfMonthWithNoDaysIsRefused(t *testing.T) {
	defer func() {
		reason, ok := recover().(string)
		if !ok {
			t.Fatal("DaysOfMonth with no days was accepted, and the expression it wrote stops the application from starting")
		}
		if !strings.Contains(reason, "DaysOfMonth") {
			t.Fatalf("the refusal says %q, and does not name the method that was called", reason)
		}
	}()

	scheduling.NewEvent(nil, "app:work", nil).DaysOfMonth()
}

func TestDaysOfMonthWritesTheDaysItWasGiven(t *testing.T) {
	e := scheduling.NewEvent(nil, "app:work", nil).DaysOfMonth(1, 15)
	if got := e.GetExpression(); got != "0 0 1,15 * *" {
		t.Fatalf("DaysOfMonth(1, 15) wrote %q, want 0 0 1,15 * *", got)
	}
}
