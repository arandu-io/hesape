package scheduling

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The days of the week, as Schedule declares them.
//
// They answer Schedule::SUNDAY through Schedule::SATURDAY, values included, so a
// number written against the Laravel constant means the same thing here.
const (
	// Sunday is 0, which is what cron calls it.
	Sunday = 0
	// Monday is 1.
	Monday = 1
	// Tuesday is 2.
	Tuesday = 2
	// Wednesday is 3.
	Wednesday = 3
	// Thursday is 4.
	Thursday = 4
	// Friday is 5.
	Friday = 5
	// Saturday is 6.
	Saturday = 6
)

// Cron sets the expression the event fires on.
//
// It answers ManagesFrequencies::cron, and every other method on this file ends
// up here.
func (e *Event) Cron(expression string) *Event {
	e.expression = expression
	return e
}

// Between runs the event only between two times of day.
//
// It answers ManagesFrequencies::between. The times are "HH:MM", and a range
// that ends before it starts crosses midnight -- "22:00" to "04:00" is the six
// hours it reads as, not the eighteen it would be if the comparison were naive.
func (e *Event) Between(startTime, endTime string) *Event {
	return e.When(e.inTimeInterval(startTime, endTime))
}

// UnlessBetween stops the event between two times of day.
//
// It answers ManagesFrequencies::unlessBetween.
func (e *Event) UnlessBetween(startTime, endTime string) *Event {
	return e.Skip(e.inTimeInterval(startTime, endTime))
}

// inTimeInterval is ManagesFrequencies::inTimeInterval.
//
// The mechanical difference: PHP resolves "now" when the filter is built, so a
// long-lived scheduler compares against the moment the schedule was declared.
// This resolves it when the filter runs, which is what the method reads as.
func (e *Event) inTimeInterval(startTime, endTime string) Filter {
	return func(context.Context) bool {
		now := time.Now()
		if e.timezone != nil {
			now = now.In(e.timezone)
		}

		start, startErr := parseTimeOfDay(now, startTime)
		end, endErr := parseTimeOfDay(now, endTime)
		if startErr != nil || endErr != nil {
			return false
		}

		if end.Before(start) {
			if start.After(now) {
				start = start.AddDate(0, 0, -1)
			} else {
				end = end.AddDate(0, 0, 1)
			}
		}
		return !now.Before(start) && !now.After(end)
	}
}

// parseTimeOfDay reads "10:00" or "19:30" as a moment on the day of now.
func parseTimeOfDay(now time.Time, value string) (time.Time, error) {
	hour, minute, err := splitTime(value)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location()), nil
}

// splitTime reads "H", "H:M" or "H:M:S" into an hour and a minute.
func splitTime(value string) (int, int, error) {
	segments := strings.Split(strings.TrimSpace(value), ":")
	hour, err := strconv.Atoi(strings.TrimSpace(segments[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("scheduling: %q is not a time of day", value)
	}
	minute := 0
	if len(segments) >= 2 {
		if minute, err = strconv.Atoi(strings.TrimSpace(segments[1])); err != nil {
			return 0, 0, fmt.Errorf("scheduling: %q is not a time of day", value)
		}
	}
	return hour, minute, nil
}

// EverySecond runs the event every second.
//
// It answers ManagesFrequencies::everySecond. The expression stays every-minute
// and the runner loops within the minute, which is what repeatEvery sets up.
func (e *Event) EverySecond() *Event { return e.repeatEvery(1) }

// EveryTwoSeconds is ManagesFrequencies::everyTwoSeconds: it runs the event
// every two seconds.
func (e *Event) EveryTwoSeconds() *Event { return e.repeatEvery(2) }

// EveryFiveSeconds is ManagesFrequencies::everyFiveSeconds: it runs the event
// every five seconds.
func (e *Event) EveryFiveSeconds() *Event { return e.repeatEvery(5) }

// EveryTenSeconds is ManagesFrequencies::everyTenSeconds: it runs the event
// every ten seconds.
func (e *Event) EveryTenSeconds() *Event { return e.repeatEvery(10) }

// EveryFifteenSeconds is ManagesFrequencies::everyFifteenSeconds: it runs the
// event every fifteen seconds.
func (e *Event) EveryFifteenSeconds() *Event { return e.repeatEvery(15) }

// EveryTwentySeconds is ManagesFrequencies::everyTwentySeconds: it runs the
// event every twenty seconds.
func (e *Event) EveryTwentySeconds() *Event { return e.repeatEvery(20) }

// EveryThirtySeconds is ManagesFrequencies::everyThirtySeconds: it runs the
// event every thirty seconds.
func (e *Event) EveryThirtySeconds() *Event { return e.repeatEvery(30) }

// repeatEvery is ManagesFrequencies::repeatEvery.
//
// A number that does not divide sixty panics, where the PHP throws: a schedule
// written in source with a repeat of seven seconds would drift every minute, and
// there is nobody to hand the error to at the point the schedule is declared.
func (e *Event) repeatEvery(seconds int) *Event {
	if seconds <= 0 || 60%seconds != 0 {
		panic(fmt.Sprintf("scheduling: the seconds [%d] are not evenly divisible by 60", seconds))
	}
	e.repeatSeconds = seconds
	return e.EveryMinute()
}

// EveryMinute runs the event every minute.
//
// It answers ManagesFrequencies::everyMinute.
func (e *Event) EveryMinute() *Event { return e.spliceIntoPosition(1, "*") }

// EveryTwoMinutes is ManagesFrequencies::everyTwoMinutes: it runs the event
// every two minutes.
func (e *Event) EveryTwoMinutes() *Event { return e.spliceIntoPosition(1, "*/2") }

// EveryThreeMinutes is ManagesFrequencies::everyThreeMinutes: it runs the event
// every three minutes.
func (e *Event) EveryThreeMinutes() *Event { return e.spliceIntoPosition(1, "*/3") }

// EveryFourMinutes is ManagesFrequencies::everyFourMinutes: it runs the event
// every four minutes.
func (e *Event) EveryFourMinutes() *Event { return e.spliceIntoPosition(1, "*/4") }

// EveryFiveMinutes is ManagesFrequencies::everyFiveMinutes: it runs the event
// every five minutes.
func (e *Event) EveryFiveMinutes() *Event { return e.spliceIntoPosition(1, "*/5") }

// EveryTenMinutes is ManagesFrequencies::everyTenMinutes: it runs the event
// every ten minutes.
func (e *Event) EveryTenMinutes() *Event { return e.spliceIntoPosition(1, "*/10") }

// EveryFifteenMinutes is ManagesFrequencies::everyFifteenMinutes: it runs the
// event every fifteen minutes.
func (e *Event) EveryFifteenMinutes() *Event { return e.spliceIntoPosition(1, "*/15") }

// EveryThirtyMinutes is ManagesFrequencies::everyThirtyMinutes: it runs the
// event every thirty minutes.
func (e *Event) EveryThirtyMinutes() *Event { return e.spliceIntoPosition(1, "*/30") }

// Hourly runs the event at the top of every hour.
//
// It answers ManagesFrequencies::hourly.
func (e *Event) Hourly() *Event { return e.spliceIntoPosition(1, "0") }

// HourlyAt runs the event every hour, at the given minutes past.
//
// It answers ManagesFrequencies::hourlyAt.
func (e *Event) HourlyAt(offset ...int) *Event { return e.hourBasedSchedule(joinInts(offset), "*") }

// EveryOddHour runs the event on every odd hour.
//
// It answers ManagesFrequencies::everyOddHour.
func (e *Event) EveryOddHour(offset ...int) *Event {
	return e.hourBasedSchedule(joinIntsOrZero(offset), "1-23/2")
}

// EveryTwoHours is ManagesFrequencies::everyTwoHours: it runs the event every
// two hours.
func (e *Event) EveryTwoHours(offset ...int) *Event {
	return e.hourBasedSchedule(joinIntsOrZero(offset), "*/2")
}

// EveryThreeHours is ManagesFrequencies::everyThreeHours: it runs the event
// every three hours.
func (e *Event) EveryThreeHours(offset ...int) *Event {
	return e.hourBasedSchedule(joinIntsOrZero(offset), "*/3")
}

// EveryFourHours is ManagesFrequencies::everyFourHours: it runs the event every
// four hours.
func (e *Event) EveryFourHours(offset ...int) *Event {
	return e.hourBasedSchedule(joinIntsOrZero(offset), "*/4")
}

// EverySixHours is ManagesFrequencies::everySixHours: it runs the event every
// six hours.
func (e *Event) EverySixHours(offset ...int) *Event {
	return e.hourBasedSchedule(joinIntsOrZero(offset), "*/6")
}

// Daily runs the event at midnight.
//
// It answers ManagesFrequencies::daily.
func (e *Event) Daily() *Event { return e.hourBasedSchedule("0", "0") }

// At runs the event daily at a time.
//
// It answers ManagesFrequencies::at, which is one call to dailyAt in the PHP
// too.
func (e *Event) At(t string) *Event { return e.DailyAt(t) }

// DailyAt runs the event daily at a time: "10:00", "19:30".
//
// It answers ManagesFrequencies::dailyAt.
func (e *Event) DailyAt(t string) *Event {
	hour, minute, err := splitTime(t)
	if err != nil {
		panic(err.Error())
	}
	return e.hourBasedSchedule(strconv.Itoa(minute), strconv.Itoa(hour))
}

// TwiceDaily runs the event at two hours of the day.
//
// It answers ManagesFrequencies::twiceDaily, defaults included: 1am and 1pm.
func (e *Event) TwiceDaily(hours ...int) *Event {
	first, second := 1, 13
	if len(hours) > 0 {
		first = hours[0]
	}
	if len(hours) > 1 {
		second = hours[1]
	}
	return e.TwiceDailyAt(first, second, 0)
}

// TwiceDailyAt runs the event at two hours of the day, at an offset in the hour.
//
// It answers ManagesFrequencies::twiceDailyAt.
func (e *Event) TwiceDailyAt(first, second, offset int) *Event {
	return e.hourBasedSchedule(strconv.Itoa(offset), fmt.Sprintf("%d,%d", first, second))
}

// hourBasedSchedule is ManagesFrequencies::hourBasedSchedule.
func (e *Event) hourBasedSchedule(minutes, hours string) *Event {
	return e.spliceIntoPosition(1, minutes).spliceIntoPosition(2, hours)
}

// Weekdays runs the event Monday to Friday.
//
// It answers ManagesFrequencies::weekdays.
func (e *Event) Weekdays() *Event {
	return e.Days(fmt.Sprintf("%d-%d", Monday, Friday))
}

// Weekends runs the event on Saturday and Sunday.
//
// It answers ManagesFrequencies::weekends.
func (e *Event) Weekends() *Event {
	return e.Days(fmt.Sprintf("%d,%d", Saturday, Sunday))
}

// Mondays is ManagesFrequencies::mondays: it runs the event only on Mondays.
func (e *Event) Mondays() *Event { return e.Days(strconv.Itoa(Monday)) }

// Tuesdays is ManagesFrequencies::tuesdays: it runs the event only on Tuesdays.
func (e *Event) Tuesdays() *Event { return e.Days(strconv.Itoa(Tuesday)) }

// Wednesdays is ManagesFrequencies::wednesdays: it runs the event only on
// Wednesdays.
func (e *Event) Wednesdays() *Event { return e.Days(strconv.Itoa(Wednesday)) }

// Thursdays is ManagesFrequencies::thursdays: it runs the event only on
// Thursdays.
func (e *Event) Thursdays() *Event { return e.Days(strconv.Itoa(Thursday)) }

// Fridays is ManagesFrequencies::fridays: it runs the event only on Fridays.
func (e *Event) Fridays() *Event { return e.Days(strconv.Itoa(Friday)) }

// Saturdays is ManagesFrequencies::saturdays: it runs the event only on
// Saturdays.
func (e *Event) Saturdays() *Event { return e.Days(strconv.Itoa(Saturday)) }

// Sundays is ManagesFrequencies::sundays: it runs the event only on Sundays.
func (e *Event) Sundays() *Event { return e.Days(strconv.Itoa(Sunday)) }

// Weekly runs the event at midnight on Sunday.
//
// It answers ManagesFrequencies::weekly.
func (e *Event) Weekly() *Event {
	return e.spliceIntoPosition(1, "0").spliceIntoPosition(2, "0").spliceIntoPosition(5, "0")
}

// WeeklyOn runs the event on a day of the week, at a time.
//
// It answers ManagesFrequencies::weeklyOn, default time included: midnight.
func (e *Event) WeeklyOn(dayOfWeek int, t ...string) *Event {
	e.DailyAt(timeOr(t))
	return e.Days(strconv.Itoa(dayOfWeek))
}

// Monthly runs the event at midnight on the first of the month.
//
// It answers ManagesFrequencies::monthly.
func (e *Event) Monthly() *Event {
	return e.spliceIntoPosition(1, "0").spliceIntoPosition(2, "0").spliceIntoPosition(3, "1")
}

// MonthlyOn runs the event on a day of the month, at a time.
//
// It answers ManagesFrequencies::monthlyOn.
func (e *Event) MonthlyOn(dayOfMonth int, t ...string) *Event {
	e.DailyAt(timeOr(t))
	return e.spliceIntoPosition(3, strconv.Itoa(dayOfMonth))
}

// TwiceMonthly runs the event on two days of the month, at a time.
//
// It answers ManagesFrequencies::twiceMonthly, defaults included: the 1st and
// the 16th, at midnight.
func (e *Event) TwiceMonthly(first, second int, t ...string) *Event {
	e.DailyAt(timeOr(t))
	return e.spliceIntoPosition(3, fmt.Sprintf("%d,%d", first, second))
}

// LastDayOfMonth runs the event on the last day of the month, at a time.
//
// It answers ManagesFrequencies::lastDayOfMonth. The PHP resolves the day when
// the schedule is declared, which is wrong in every month of a different length
// from the one the process booted in; this resolves it when the expression is
// tested, by asking for the 28th through the 31st and letting the runner's own
// filter drop the ones that are not the last.
func (e *Event) LastDayOfMonth(t ...string) *Event {
	e.DailyAt(timeOr(t))
	e.When(func(context.Context) bool {
		now := time.Now()
		if e.timezone != nil {
			now = now.In(e.timezone)
		}
		return now.AddDate(0, 0, 1).Day() == 1
	})
	return e.spliceIntoPosition(3, "28-31")
}

// DaysOfMonth runs the event on the given days of the month, at midnight.
//
// It answers ManagesFrequencies::daysOfMonth.
//
// Naming no day panics. It used to splice the "0" that joinInts falls back to,
// and "0 0 0 * *" is not a cron expression: there is no zeroth day of the month,
// so Schedule refused the expression at registration and the application did not
// start. The PHP writes an empty field there and its parser throws for the same
// reason; the mistake is the empty call, and this is the last place it can still
// be named.
func (e *Event) DaysOfMonth(days ...int) *Event {
	if len(days) == 0 {
		panic("scheduling: DaysOfMonth needs at least one day of the month")
	}
	e.DailyAt("0:0")
	return e.spliceIntoPosition(3, joinInts(days))
}

// Quarterly runs the event at midnight on the first day of each quarter.
//
// It answers ManagesFrequencies::quarterly.
func (e *Event) Quarterly() *Event {
	return e.spliceIntoPosition(1, "0").
		spliceIntoPosition(2, "0").
		spliceIntoPosition(3, "1").
		spliceIntoPosition(4, "1-12/3")
}

// QuarterlyOn runs the event on a day of each quarter, at a time.
//
// It answers ManagesFrequencies::quarterlyOn.
func (e *Event) QuarterlyOn(dayOfQuarter int, t ...string) *Event {
	e.DailyAt(timeOr(t))
	return e.spliceIntoPosition(3, strconv.Itoa(dayOfQuarter)).spliceIntoPosition(4, "1-12/3")
}

// Yearly runs the event at midnight on the first of January.
//
// It answers ManagesFrequencies::yearly.
func (e *Event) Yearly() *Event {
	return e.spliceIntoPosition(1, "0").
		spliceIntoPosition(2, "0").
		spliceIntoPosition(3, "1").
		spliceIntoPosition(4, "1")
}

// YearlyOn runs the event on a month and day, at a time.
//
// It answers ManagesFrequencies::yearlyOn.
func (e *Event) YearlyOn(month, dayOfMonth int, t ...string) *Event {
	e.DailyAt(timeOr(t))
	return e.spliceIntoPosition(3, strconv.Itoa(dayOfMonth)).spliceIntoPosition(4, strconv.Itoa(month))
}

// Days limits the event to days of the week.
//
// It answers ManagesFrequencies::days. It takes the field as written, so both
// Days("1-5") and Days("1", "3") say what they read as.
func (e *Event) Days(days ...string) *Event {
	return e.spliceIntoPosition(5, strings.Join(days, ","))
}

// Timezone sets the timezone the expression is evaluated in.
//
// It answers ManagesFrequencies::timezone.
func (e *Event) Timezone(timezone *time.Location) *Event {
	e.timezone = timezone
	return e
}

// spliceIntoPosition replaces one field of the expression.
//
// It answers ManagesFrequencies::spliceIntoPosition, one-based position
// included: 1 is the minute and 5 is the day of the week.
func (e *Event) spliceIntoPosition(position int, value string) *Event {
	segments := strings.Fields(e.expression)
	for len(segments) < 5 {
		segments = append(segments, "*")
	}
	if position >= 1 && position <= len(segments) {
		segments[position-1] = value
	}
	return e.Cron(strings.Join(segments, " "))
}

// joinInts renders a list of numbers as a cron field, and "0" when empty.
func joinInts(values []int) string {
	if len(values) == 0 {
		return "0"
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

// joinIntsOrZero is joinInts, spelt for the methods whose PHP default is 0.
func joinIntsOrZero(values []int) string { return joinInts(values) }

// timeOr is the "0:0" default the monthly and weekly methods carry.
func timeOr(t []string) string {
	if len(t) > 0 && strings.TrimSpace(t[0]) != "" {
		return t[0]
	}
	return "0:0"
}
