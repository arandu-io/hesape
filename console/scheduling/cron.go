package scheduling

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronExpression is a parsed five-field cron expression.
//
// It is written here rather than pulled in as a dependency, because the
// module declares only one and this is eighty lines.
//
// Five fields, not six: no seconds. Sub-minute repetition is
// Event.RepeatSeconds -- the expression stays every-minute and
// Runner.repeatEvents loops within the minute.
//
// What it supports is the syntax people write: `*`, `5`, `1-5`, `*/15`,
// `1,15,30`, and the shorthands `@hourly`, `@daily`, `@midnight`, `@weekly`
// and `@monthly`.
type CronExpression struct {
	minute  fieldSet
	hour    fieldSet
	day     fieldSet
	month   fieldSet
	weekday fieldSet
	spec    string
}

// String returns the expression it was parsed from, for schedule:list.
func (c CronExpression) String() string { return c.spec }

// fieldSet is which values of one field match. Fixed size because every cron
// field has a small known range, and a bitmap over a map is one allocation
// versus none.
type fieldSet [60]bool

// ParseCronExpression reads a cron expression.
func ParseCronExpression(spec string) (CronExpression, error) {
	trimmed := strings.TrimSpace(spec)

	switch trimmed {
	case "@hourly":
		trimmed = "0 * * * *"
	case "@daily", "@midnight":
		trimmed = "0 0 * * *"
	case "@weekly":
		trimmed = "0 0 * * 0"
	case "@monthly":
		trimmed = "0 0 1 * *"
	}

	fields := strings.Fields(trimmed)
	if len(fields) != 5 {
		return CronExpression{}, fmt.Errorf(
			"a cron expression has five fields (minute hour day month weekday), and %q has %d",
			spec, len(fields))
	}

	c := CronExpression{spec: strings.TrimSpace(spec)}
	for _, f := range []struct {
		name     string
		text     string
		min, max int
		into     *fieldSet
	}{
		{"minute", fields[0], 0, 59, &c.minute},
		{"hour", fields[1], 0, 23, &c.hour},
		{"day of month", fields[2], 1, 31, &c.day},
		{"month", fields[3], 1, 12, &c.month},
		{"day of week", fields[4], 0, 6, &c.weekday},
	} {
		if err := parseField(f.text, f.min, f.max, f.into); err != nil {
			return CronExpression{}, fmt.Errorf("%s in %q: %w", f.name, spec, err)
		}
	}
	return c, nil
}

// MustParseCronExpression is ParseCronExpression for a constant.
//
// It panics, which is right for an expression written in source: a schedule
// nobody can parse must not reach the runner.
func MustParseCronExpression(spec string) CronExpression {
	c, err := ParseCronExpression(spec)
	if err != nil {
		panic("scheduling: " + err.Error())
	}
	return c
}

func parseField(text string, min, max int, into *fieldSet) error {
	for _, part := range strings.Split(text, ",") {
		step := 1
		if base, stepText, hasStep := strings.Cut(part, "/"); hasStep {
			parsed, err := strconv.Atoi(stepText)
			if err != nil || parsed < 1 {
				return fmt.Errorf("%q is not a step", stepText)
			}
			step = parsed
			part = base
		}

		low, high := min, max
		switch {
		case part == "*":
		case strings.Contains(part, "-"):
			from, to, _ := strings.Cut(part, "-")
			var err error
			if low, err = value(from, min, max); err != nil {
				return err
			}
			if high, err = value(to, min, max); err != nil {
				return err
			}
			if low > high {
				return fmt.Errorf("%q counts backwards", part)
			}
		default:
			v, err := value(part, min, max)
			if err != nil {
				return err
			}
			low, high = v, v
		}

		for i := low; i <= high; i += step {
			into[i] = true
		}
	}
	return nil
}

func value(text string, min, max int) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", text)
	}
	if v < min || v > max {
		return 0, fmt.Errorf("%d is outside %d-%d", v, min, max)
	}
	return v, nil
}

// IsDue reports whether the expression fires in the minute of t.
//
// Day-of-month and day-of-week are OR when both are restricted, which is the
// behaviour of every cron since Vixie: "0 0 1 * 1" means the first of the
// month AND every Monday, not their intersection. It surprises people, and
// matching the surprise is better than being the one implementation that
// differs.
func (c CronExpression) IsDue(t time.Time) bool {
	if !c.minute[t.Minute()] || !c.hour[t.Hour()] || !c.month[int(t.Month())] {
		return false
	}

	dayRestricted := !full(c.day, 1, 31)
	weekdayRestricted := !full(c.weekday, 0, 6)

	switch {
	case dayRestricted && weekdayRestricted:
		return c.day[t.Day()] || c.weekday[int(t.Weekday())]
	case dayRestricted:
		return c.day[t.Day()]
	case weekdayRestricted:
		return c.weekday[int(t.Weekday())]
	default:
		return true
	}
}

// GetNextRunDate returns the first minute after t that matches.
//
// It walks minute by minute, bounded to a year: an expression that matches
// nothing in a year matches nothing at all -- February 30th, for instance --
// and returning the zero time is what lets schedule:list say so instead of
// hanging.
func (c CronExpression) GetNextRunDate(t time.Time) time.Time {
	at := t.Truncate(time.Minute)
	for range 366 * 24 * 60 {
		at = at.Add(time.Minute)
		if c.IsDue(at) {
			return at
		}
	}
	return time.Time{}
}

func full(set fieldSet, min, max int) bool {
	for i := min; i <= max; i++ {
		if !set[i] {
			return false
		}
	}
	return true
}
