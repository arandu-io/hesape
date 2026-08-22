package scheduling_test

import (
	"testing"
	"time"

	"github.com/arandu-io/hesape/console/scheduling"
)

// event returns a bare event to write a frequency onto.
func event() *scheduling.Event { return scheduling.NewEvent(nil, "app:work", nil) }

func TestAnEventStartsOnEveryMinute(t *testing.T) {
	if got := event().GetExpression(); got != "* * * * *" {
		t.Errorf("expression = %q, want * * * * *", got)
	}
}

func TestEachFrequencyMethodWritesItsCronExpression(t *testing.T) {
	cases := []struct {
		name  string
		build func(*scheduling.Event) *scheduling.Event
		want  string
	}{
		{"EveryMinute", (*scheduling.Event).EveryMinute, "* * * * *"},
		{"EveryTwoMinutes", (*scheduling.Event).EveryTwoMinutes, "*/2 * * * *"},
		{"EveryThreeMinutes", (*scheduling.Event).EveryThreeMinutes, "*/3 * * * *"},
		{"EveryFourMinutes", (*scheduling.Event).EveryFourMinutes, "*/4 * * * *"},
		{"EveryFiveMinutes", (*scheduling.Event).EveryFiveMinutes, "*/5 * * * *"},
		{"EveryTenMinutes", (*scheduling.Event).EveryTenMinutes, "*/10 * * * *"},
		{"EveryFifteenMinutes", (*scheduling.Event).EveryFifteenMinutes, "*/15 * * * *"},
		{"EveryThirtyMinutes", (*scheduling.Event).EveryThirtyMinutes, "*/30 * * * *"},
		{"Hourly", (*scheduling.Event).Hourly, "0 * * * *"},
		{"Daily", (*scheduling.Event).Daily, "0 0 * * *"},
		{"Weekly", (*scheduling.Event).Weekly, "0 0 * * 0"},
		{"Monthly", (*scheduling.Event).Monthly, "0 0 1 * *"},
		{"Quarterly", (*scheduling.Event).Quarterly, "0 0 1 1-12/3 *"},
		{"Yearly", (*scheduling.Event).Yearly, "0 0 1 1 *"},
		{"Weekdays", (*scheduling.Event).Weekdays, "* * * * 1-5"},
		{"Weekends", (*scheduling.Event).Weekends, "* * * * 6,0"},
		{"Mondays", (*scheduling.Event).Mondays, "* * * * 1"},
		{"Tuesdays", (*scheduling.Event).Tuesdays, "* * * * 2"},
		{"Wednesdays", (*scheduling.Event).Wednesdays, "* * * * 3"},
		{"Thursdays", (*scheduling.Event).Thursdays, "* * * * 4"},
		{"Fridays", (*scheduling.Event).Fridays, "* * * * 5"},
		{"Saturdays", (*scheduling.Event).Saturdays, "* * * * 6"},
		{"Sundays", (*scheduling.Event).Sundays, "* * * * 0"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.build(event()).GetExpression(); got != c.want {
				t.Errorf("%s wrote %q, want %q", c.name, got, c.want)
			}
		})
	}
}

func TestDailyAtWritesTheHourAndTheMinute(t *testing.T) {
	if got := event().DailyAt("19:30").GetExpression(); got != "30 19 * * *" {
		t.Errorf("DailyAt(19:30) wrote %q, want 30 19 * * *", got)
	}
	// A time with no minute is on the hour, which is what explode(':') leaves.
	if got := event().DailyAt("7").GetExpression(); got != "0 7 * * *" {
		t.Errorf("DailyAt(7) wrote %q, want 0 7 * * *", got)
	}
}

func TestAtIsDailyAt(t *testing.T) {
	if got := event().At("06:15").GetExpression(); got != "15 6 * * *" {
		t.Errorf("At wrote %q, want 15 6 * * *", got)
	}
}

func TestHourlyAtWritesTheOffsets(t *testing.T) {
	if got := event().HourlyAt(15).GetExpression(); got != "15 * * * *" {
		t.Errorf("HourlyAt(15) wrote %q, want 15 * * * *", got)
	}
	if got := event().HourlyAt(15, 45).GetExpression(); got != "15,45 * * * *" {
		t.Errorf("HourlyAt(15, 45) wrote %q, want 15,45 * * * *", got)
	}
}

func TestTwiceDailyDefaultsToOneAmAndOnePm(t *testing.T) {
	if got := event().TwiceDaily().GetExpression(); got != "0 1,13 * * *" {
		t.Errorf("TwiceDaily wrote %q, want 0 1,13 * * *", got)
	}
	if got := event().TwiceDailyAt(3, 15, 30).GetExpression(); got != "30 3,15 * * *" {
		t.Errorf("TwiceDailyAt wrote %q, want 30 3,15 * * *", got)
	}
}

func TestWeeklyOnAndMonthlyOnWriteTheDayAndTheTime(t *testing.T) {
	if got := event().WeeklyOn(scheduling.Monday, "8:00").GetExpression(); got != "0 8 * * 1" {
		t.Errorf("WeeklyOn wrote %q, want 0 8 * * 1", got)
	}
	if got := event().MonthlyOn(15, "8:00").GetExpression(); got != "0 8 15 * *" {
		t.Errorf("MonthlyOn wrote %q, want 0 8 15 * *", got)
	}
	if got := event().TwiceMonthly(1, 16, "8:00").GetExpression(); got != "0 8 1,16 * *" {
		t.Errorf("TwiceMonthly wrote %q, want 0 8 1,16 * *", got)
	}
	if got := event().YearlyOn(6, 12, "8:00").GetExpression(); got != "0 8 12 6 *" {
		t.Errorf("YearlyOn wrote %q, want 0 8 12 6 *", got)
	}
	if got := event().QuarterlyOn(5, "8:00").GetExpression(); got != "0 8 5 1-12/3 *" {
		t.Errorf("QuarterlyOn wrote %q, want 0 8 5 1-12/3 *", got)
	}
}

func TestDaysOfMonthWritesTheList(t *testing.T) {
	if got := event().DaysOfMonth(1, 10, 20).GetExpression(); got != "0 0 1,10,20 * *" {
		t.Errorf("DaysOfMonth wrote %q, want 0 0 1,10,20 * *", got)
	}
}

func TestEveryOddHourAndTheHourStepsWriteTheHourField(t *testing.T) {
	cases := map[string]struct {
		build func(*scheduling.Event) *scheduling.Event
		want  string
	}{
		"EveryOddHour":    {func(e *scheduling.Event) *scheduling.Event { return e.EveryOddHour() }, "0 1-23/2 * * *"},
		"EveryTwoHours":   {func(e *scheduling.Event) *scheduling.Event { return e.EveryTwoHours() }, "0 */2 * * *"},
		"EveryThreeHours": {func(e *scheduling.Event) *scheduling.Event { return e.EveryThreeHours() }, "0 */3 * * *"},
		"EveryFourHours":  {func(e *scheduling.Event) *scheduling.Event { return e.EveryFourHours() }, "0 */4 * * *"},
		"EverySixHours":   {func(e *scheduling.Event) *scheduling.Event { return e.EverySixHours() }, "0 */6 * * *"},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := c.build(event()).GetExpression(); got != c.want {
				t.Errorf("%s wrote %q, want %q", name, got, c.want)
			}
		})
	}
}

func TestTheSubMinuteMethodsLeaveTheExpressionOnEveryMinute(t *testing.T) {
	e := event().EveryFifteenSeconds()

	if got := e.GetExpression(); got != "* * * * *" {
		t.Errorf("expression = %q, want * * * * *: the repeat is not a cron field", got)
	}
	if !e.IsRepeatable() {
		t.Error("EveryFifteenSeconds is repeatable")
	}
	if got := e.RepeatSeconds(); got != 15 {
		t.Errorf("RepeatSeconds = %d, want 15", got)
	}
}

func TestEverySubMinuteRepeatDividesTheMinuteEvenly(t *testing.T) {
	// A repeat that does not divide sixty drifts every minute, which is what
	// repeatEvery refuses. Every one the package ships has to pass its own
	// guard, and this is what says so.
	cases := map[int]func(*scheduling.Event) *scheduling.Event{
		1:  (*scheduling.Event).EverySecond,
		2:  (*scheduling.Event).EveryTwoSeconds,
		5:  (*scheduling.Event).EveryFiveSeconds,
		10: (*scheduling.Event).EveryTenSeconds,
		15: (*scheduling.Event).EveryFifteenSeconds,
		20: (*scheduling.Event).EveryTwentySeconds,
		30: (*scheduling.Event).EveryThirtySeconds,
	}

	for seconds, build := range cases {
		if 60%seconds != 0 {
			t.Fatalf("%d does not divide 60", seconds)
		}
		if got := build(event()).RepeatSeconds(); got != seconds {
			t.Errorf("the every-%d-seconds method set %d", seconds, got)
		}
	}
}

func TestTimezoneIsWhatTheExpressionIsEvaluatedIn(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("no timezone database: %v", err)
	}

	e := event().DailyAt("09:00").Timezone(tokyo)
	if e.GetTimezone() != tokyo {
		t.Error("the timezone was not recorded")
	}

	// Midnight UTC is nine in Tokyo, so the event is due then and not at nine
	// UTC.
	midnightUTC := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	if !e.IsDue(midnightUTC, "production", false) {
		t.Error("an event at 09:00 Tokyo was not due at 00:00 UTC")
	}
	if e.IsDue(midnightUTC.Add(9*time.Hour), "production", false) {
		t.Error("an event at 09:00 Tokyo was due at 09:00 UTC")
	}
}

func TestEnvironmentsLimitWhereTheEventRuns(t *testing.T) {
	e := event().Daily().Environments("production")

	if !e.RunsInEnvironment("production") {
		t.Error("the event names production and said it does not run there")
	}
	if e.RunsInEnvironment("local") {
		t.Error("the event names production only and said it runs in local")
	}
	if !event().RunsInEnvironment("anything") {
		t.Error("an event that named no environment runs in all of them")
	}
}

func TestMaintenanceModeStopsAnEventUnlessItSaidOtherwise(t *testing.T) {
	at := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)

	if event().Daily().IsDue(at.Truncate(24*time.Hour), "production", true) {
		t.Error("an ordinary event ran while the application was down")
	}
	if !event().Daily().EvenInMaintenanceMode().IsDue(at.Truncate(24*time.Hour), "production", true) {
		t.Error("an event that said even in maintenance mode did not run")
	}
}
