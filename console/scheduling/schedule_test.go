package scheduling_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/console/events"
	"github.com/arandu-io/hesape/console/scheduling"
)

func TestCronExpressionParsesTheShorthands(t *testing.T) {
	cases := map[string]string{
		"@hourly":   "0 * * * *",
		"@daily":    "0 0 * * *",
		"@midnight": "0 0 * * *",
		"@weekly":   "0 0 * * 0",
		"@monthly":  "0 0 1 * *",
	}

	for shorthand, equivalent := range cases {
		short := scheduling.MustParseCronExpression(shorthand)
		long := scheduling.MustParseCronExpression(equivalent)

		for _, at := range []time.Time{
			time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 8, 11, 13, 0, 0, 0, time.UTC),
			time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		} {
			if short.IsDue(at) != long.IsDue(at) {
				t.Errorf("%s and %s disagree at %s", shorthand, equivalent, at)
			}
		}
	}
}

func TestCronExpressionRefusesAnExpressionThatIsNotFiveFields(t *testing.T) {
	for _, spec := range []string{"", "* * *", "* * * * * *", "60 * * * *", "* * * * 9"} {
		if _, err := scheduling.ParseCronExpression(spec); err == nil {
			t.Errorf("ParseCronExpression(%q) returned no error", spec)
		}
	}
}

func TestCronExpressionOrsTheDayFieldsWhenBothAreRestricted(t *testing.T) {
	// Every cron since Vixie reads "0 0 1 * 1" as the first of the month OR
	// every Monday, not their intersection.
	expression := scheduling.MustParseCronExpression("0 0 1 * 1")

	firstOfMonth := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if !expression.IsDue(firstOfMonth) {
		t.Error("the first of the month did not match")
	}
	monday := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	if monday.Weekday() != time.Monday {
		t.Fatalf("%s is not a Monday", monday)
	}
	if !expression.IsDue(monday) {
		t.Error("a Monday that is not the first did not match")
	}
	tuesday := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	if expression.IsDue(tuesday) {
		t.Error("a Tuesday that is not the first matched")
	}
}

func TestCronExpressionGetNextRunDateWalksForward(t *testing.T) {
	expression := scheduling.MustParseCronExpression("0 3 * * *")
	from := time.Date(2026, 8, 11, 4, 0, 0, 0, time.UTC)

	next := expression.GetNextRunDate(from)
	want := time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("next = %s, want %s", next, want)
	}
}

func TestScheduleCollectsTheEventsItDeclares(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)
	s.Exec("app:one").Daily()
	s.Exec("app:two").Hourly()

	if got := len(s.Events()); got != 2 {
		t.Fatalf("got %d events, want 2", got)
	}
	if got := s.Events()[0].GetExpression(); got != "0 0 * * *" {
		t.Errorf("the first event fires on %q, want 0 0 * * *", got)
	}
}

func TestDueEventsReturnsOnlyWhatIsDue(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)
	s.Environment = "production"
	s.Exec("app:nightly").DailyAt("03:00")
	s.Exec("app:always").EveryMinute()

	at := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
	if got := len(s.DueEvents(at)); got != 2 {
		t.Errorf("at 03:00 got %d due, want 2", got)
	}

	at = time.Date(2026, 8, 11, 4, 0, 0, 0, time.UTC)
	if got := len(s.DueEvents(at)); got != 1 {
		t.Errorf("at 04:00 got %d due, want 1", got)
	}
}

func TestAGroupMergesItsAttributesIntoEveryEvent(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)

	s.Attributes().Daily()
	s.Group(func(s *scheduling.Schedule) {
		s.Exec("app:one")
		s.Exec("app:two")
	})

	for _, event := range s.Events() {
		if got := event.GetExpression(); got != "0 0 * * *" {
			t.Errorf("%s fires on %q, want 0 0 * * *", event.Command, got)
		}
	}
}

func TestAttributesNamedForOneEventDoNotLeakIntoTheNext(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)

	s.Attributes().Hourly()
	s.Exec("app:one")
	s.Exec("app:two")

	if got := s.Events()[0].GetExpression(); got != "0 * * * *" {
		t.Errorf("the first event fires on %q, want 0 * * * *", got)
	}
	if got := s.Events()[1].GetExpression(); got != "* * * * *" {
		t.Errorf("the second event fires on %q, want * * * * *: the attributes were consumed", got)
	}
}

func TestGroupWithNoAttributesIsRefused(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a group with nothing to merge was accepted, and it would do nothing")
		}
	}()
	scheduling.NewSchedule(nil, nil, nil).Group(func(*scheduling.Schedule) {})
}

func TestACallbackEventReceivesTheGrantItWasDeclaredWith(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)

	var seen auth.Grant
	s.Call(func(_ context.Context, g auth.Grant) error {
		seen = g
		return nil
	}).Name("close the invoices").Action("invoice:close").Tenant("acme").EveryMinute()

	if err := s.Events()[0].Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := auth.Tenant(seen); got != "acme" {
		t.Errorf("the callback got the tenant %q, want acme", got)
	}
}

func TestACallbackEventReportsWhatTheCallbackFailedWith(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)
	boom := errors.New("boom")

	e := s.Call(func(context.Context, auth.Grant) error { return boom })
	e.Name("failing").EveryMinute()

	if err := e.Run(t.Context()); !errors.Is(err, boom) {
		t.Errorf("Run returned %v, want boom", err)
	}
	if e.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", e.ExitCode)
	}
}

func TestACallbackEventRunsItsAfterCallbacks(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)

	ran := false
	e := s.Call(func(context.Context, auth.Grant) error { return nil })
	e.Name("counting").Then(func(context.Context) error { ran = true; return nil })

	if err := e.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !ran {
		t.Error("the after callback did not run")
	}
}

func TestACallbackEventCannotBeSentToTheBackground(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a closure was sent to the background, and the background is another process")
		}
	}()
	scheduling.NewSchedule(nil, nil, nil).
		Call(func(context.Context, auth.Grant) error { return nil }).
		RunInBackground()
}

func TestWithoutOverlappingNeedsANameOnAClosure(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("an unnamed closure asked not to overlap, and there is nothing to name its lock")
		}
	}()
	scheduling.NewSchedule(nil, nil, nil).
		Call(func(context.Context, auth.Grant) error { return nil }).
		WithoutOverlapping()
}

func TestAFilterStopsAnEventFromRunning(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)

	ran := false
	s.Call(func(context.Context, auth.Grant) error { ran = true; return nil }).
		EveryMinute().
		WhenTrue(false)

	runner := scheduling.NewRunner(s)
	if got := runner.Run(t.Context(), time.Now()); got != 0 {
		t.Errorf("the runner ran %d events, want 0", got)
	}
	if ran {
		t.Error("an event whose filter said no ran anyway")
	}
}

func TestTheRunnerReportsWhatItSkipped(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)
	s.Call(func(context.Context, auth.Grant) error { return nil }).
		Name("skipped one").
		EveryMinute().
		SkipTrue(true)

	var reported []any
	runner := scheduling.NewRunner(s)
	runner.Listen = func(event any) { reported = append(reported, event) }
	runner.Run(t.Context(), time.Now())

	if len(reported) != 1 {
		t.Fatalf("the runner reported %d events, want 1", len(reported))
	}
	if _, isSkip := reported[0].(events.ScheduledTaskSkipped); !isSkip {
		t.Errorf("the runner reported %T, want ScheduledTaskSkipped", reported[0])
	}
}

func TestAPerTenantEventRunsOncePerTenantWithItsOwnGrant(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)
	s.Tenants = func(context.Context) ([]string, error) { return []string{"acme", "globex"}, nil }

	var seen []string
	s.Call(func(_ context.Context, g auth.Grant) error {
		seen = append(seen, auth.Tenant(g))
		return nil
	}).Name("per tenant").Action("invoice:close").PerTenant().EveryMinute()

	if got := scheduling.NewRunner(s).Run(t.Context(), time.Now()); got != 2 {
		t.Errorf("the runner ran %d times, want 2", got)
	}
	if strings.Join(seen, ",") != "acme,globex" {
		t.Errorf("the tenants seen were %v, want [acme globex]", seen)
	}
}

func TestAPerTenantEventWithNoResolverIsReportedRatherThanSilent(t *testing.T) {
	s := scheduling.NewSchedule(nil, nil, nil)
	s.Call(func(context.Context, auth.Grant) error { return nil }).
		Name("per tenant").
		PerTenant().
		EveryMinute()

	var failed *events.ScheduledTaskFailed
	runner := scheduling.NewRunner(s)
	runner.Listen = func(event any) {
		if e, is := event.(events.ScheduledTaskFailed); is {
			failed = &e
		}
	}
	runner.Run(t.Context(), time.Now())

	if failed == nil {
		t.Fatal("a per-tenant event with no resolver ran silently, and it would never run at all")
	}
}

// TestTheCommandBuilderCarriesNoRedirection is the change that took the shell
// out: where the output goes is not part of the command any more, so there is
// nothing in the argument list for a shell to have to read.
func TestTheCommandBuilderCarriesNoRedirection(t *testing.T) {
	e := scheduling.NewEvent(nil, "app:work --queue=mail", nil).SendOutputTo("/tmp/out.log")

	got, err := e.BuildCommand()
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if want := []string{"app:work", "--queue=mail"}; !slices.Equal(got, want) {
		t.Errorf("BuildCommand = %q, want %q", got, want)
	}
}

// TestTheCommandBuilderRunsAsAnotherUserWhenAsked pins the one wrapper that
// stayed, and the form it takes: sudo runs the program after --, so there is no
// `sh -c` and no line for it to parse.
func TestTheCommandBuilderRunsAsAnotherUserWhenAsked(t *testing.T) {
	e := scheduling.NewEvent(nil, "app:work", nil).User("deploy")

	got, err := e.BuildCommand()
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if want := []string{"sudo", "-u", "deploy", "--", "app:work"}; !slices.Equal(got, want) {
		t.Errorf("BuildCommand = %q, want %q", got, want)
	}
}

func TestTwoEventsWithTheSameExpressionAndCommandShareAMutexName(t *testing.T) {
	first := scheduling.NewEvent(nil, "app:work", nil).Daily()
	second := scheduling.NewEvent(nil, "app:work", nil).Daily()

	if first.MutexName() != second.MutexName() {
		t.Error("the same event on two replicas resolved to two locks, and neither would ever lose the race")
	}

	other := scheduling.NewEvent(nil, "app:other", nil).Daily()
	if first.MutexName() == other.MutexName() {
		t.Error("two different events share a lock")
	}
}

func TestCreateMutexNameUsingOverridesTheName(t *testing.T) {
	e := scheduling.NewEvent(nil, "app:work", nil).
		CreateMutexNameUsing(func(*scheduling.Event) string { return "chosen" })

	if got := e.MutexName(); got != "chosen" {
		t.Errorf("MutexName = %q, want chosen", got)
	}
}

func TestGetSummaryForDisplayPrefersTheDescription(t *testing.T) {
	described := scheduling.NewEvent(nil, "app:work", nil).Name("close the invoices")
	if got := described.GetSummaryForDisplay(); got != "close the invoices" {
		t.Errorf("summary = %q, want the description", got)
	}

	bare := scheduling.NewEvent(nil, "app:work", nil)
	if got := bare.GetSummaryForDisplay(); !strings.Contains(got, "app:work") {
		t.Errorf("summary = %q, want the command line", got)
	}
}
