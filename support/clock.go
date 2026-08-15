package support

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Clock answers to what Carbon is used for in Illuminate\Support\Carbon and
// Illuminate\Support\DateFactory: the one place "now" comes from, so a test can
// replace it.
//
// Carbon itself is not mirrored as a type. time.Time already is the value, and
// a second date type would be a second way to hold an instant. What is mirrored
// is the seam Carbon opens with setTestNow: [Now], [SetTestNow], [Travel],
// [TravelTo], [TravelBack] and [FreezeTime] keep the names a test types.
type Clock interface {
	// Now answers to Carbon::now.
	Now() time.Time
}

// SystemClock is the Clock the framework runs on outside a test: time.Now.
type SystemClock struct{}

// Now answers to Carbon::now, read from the operating system.
func (SystemClock) Now() time.Time { return time.Now() }

var (
	clockMu sync.RWMutex
	clock   Clock = SystemClock{}
	testNow *time.Time
)

// Now answers to Carbon::now and to Date::now: the current instant, or the
// instant a test pinned with [SetTestNow].
func Now() time.Time {
	clockMu.RLock()
	defer clockMu.RUnlock()
	if testNow != nil {
		return *testNow
	}
	return clock.Now()
}

// Today answers to Carbon::today: midnight of the current day, in the current
// location.
func Today() time.Time {
	n := Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())
}

// Tomorrow answers to Carbon::tomorrow.
func Tomorrow() time.Time { return Today().AddDate(0, 0, 1) }

// Yesterday answers to Carbon::yesterday.
func Yesterday() time.Time { return Today().AddDate(0, 0, -1) }

// parseLayouts are the shapes Carbon::parse accepts that a Go program actually
// writes. Carbon leans on strtotime, which has no equivalent in the standard
// library; these are tried in order.
var parseLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02",
	"15:04:05",
	"15:04",
	time.RFC1123Z,
	time.RFC1123,
}

// Parse answers to Carbon::parse. The PHP throws InvalidFormatException on a
// string it cannot read, so this returns (time.Time, error).
//
// A time-only string is placed on the day [Now] reports, which is what Carbon
// does.
func Parse(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Now(), nil
	}
	if strings.EqualFold(value, "now") {
		return Now(), nil
	}
	if strings.EqualFold(value, "today") {
		return Today(), nil
	}
	if strings.EqualFold(value, "tomorrow") {
		return Tomorrow(), nil
	}
	if strings.EqualFold(value, "yesterday") {
		return Yesterday(), nil
	}
	for _, layout := range parseLayouts {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err != nil {
			continue
		}
		if parsed.Year() == 0 {
			n := Now()
			return time.Date(n.Year(), n.Month(), n.Day(),
				parsed.Hour(), parsed.Minute(), parsed.Second(), parsed.Nanosecond(), n.Location()), nil
		}
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("support: could not parse date [%s]", value)
}

// CreateFromFormat answers to Carbon::createFromFormat, with a Go layout in
// place of the PHP format string; the PHP throws, so this returns an error.
func CreateFromFormat(layout, value string) (time.Time, error) {
	return time.ParseInLocation(layout, value, time.Local)
}

// CreateFromTimestamp answers to Carbon::createFromTimestamp: seconds since the
// epoch.
func CreateFromTimestamp(timestamp int64) time.Time {
	return time.Unix(timestamp, 0)
}

// CreateFromTimestampMs answers to Carbon::createFromTimestampMs.
func CreateFromTimestampMs(timestamp int64) time.Time {
	return time.UnixMilli(timestamp)
}

// ErrNotAnOrderedID is what Carbon::createFromId hits through Ulid::fromString
// and Uuid::fromString when the string is neither.
var ErrNotAnOrderedID = errors.New("support: id is neither an ordered UUID nor a ULID")

// crockford is the alphabet a ULID is written in, which leaves out I, L, O and
// U so that no two characters can be misread for each other.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// CreateFromId answers to Carbon::createFromId: the instant an ordered UUID or
// a ULID was made at, which both carry in their first 48 bits.
//
// The PHP leans on ramsey/uuid and symfony/uid; the milliseconds are read here
// instead, because the core carries no third-party code. A string that is
// neither is [ErrNotAnOrderedID], which is what the PHP throws.
func CreateFromId(id string) (time.Time, error) {
	if milliseconds, ok := ulidMilliseconds(id); ok {
		return time.UnixMilli(milliseconds), nil
	}
	if milliseconds, ok := orderedUUIDMilliseconds(id); ok {
		return time.UnixMilli(milliseconds), nil
	}
	return time.Time{}, ErrNotAnOrderedID
}

// ulidMilliseconds is Ulid::isValid followed by getDateTime: 26 Crockford
// characters, of which the first 10 are the milliseconds.
func ulidMilliseconds(id string) (int64, bool) {
	if len(id) != 26 {
		return 0, false
	}
	milliseconds := int64(0)
	for i, r := range strings.ToUpper(id) {
		index := strings.IndexRune(crockford, r)
		if index < 0 {
			return 0, false
		}
		if i < 10 {
			milliseconds = milliseconds<<5 | int64(index)
		}
	}
	return milliseconds, true
}

// orderedUUIDMilliseconds is Uuid::getDateTime for the ordered shapes: the
// first 48 bits are the milliseconds, which is version 7 and the ordered UUID
// Laravel writes.
func orderedUUIDMilliseconds(id string) (int64, bool) {
	hex := strings.ReplaceAll(id, "-", "")
	if len(hex) != 32 {
		return 0, false
	}
	milliseconds, err := strconv.ParseInt(hex[:12], 16, 64)
	if err != nil {
		return 0, false
	}
	for _, r := range hex {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return 0, false
		}
	}
	return milliseconds, true
}

// SetTestNow answers to Carbon::setTestNow: pin the instant every later [Now]
// reports. A nil value is the PHP null and hands time back to the clock.
func SetTestNow(value *time.Time) {
	clockMu.Lock()
	defer clockMu.Unlock()
	if value == nil {
		testNow = nil
		return
	}
	pinned := *value
	testNow = &pinned
}

// GetTestNow answers to Carbon::getTestNow, nil when nothing is pinned.
func GetTestNow() *time.Time {
	clockMu.RLock()
	defer clockMu.RUnlock()
	if testNow == nil {
		return nil
	}
	pinned := *testNow
	return &pinned
}

// HasTestNow answers to Carbon::hasTestNow.
func HasTestNow() bool { return GetTestNow() != nil }

// WithTestNow answers to Carbon::withTestNow: run the callback with the instant
// pinned, then put back whatever was pinned before.
func WithTestNow(value time.Time, callback func()) {
	previous := GetTestNow()
	SetTestNow(&value)
	defer SetTestNow(previous)
	callback()
}

// Use answers to DateFactory::use: the Clock every later [Now] reads. The PHP
// takes a class name, a callable or a Carbon factory; here it is the one seam
// that matters, the Clock.
func Use(c Clock) {
	clockMu.Lock()
	defer clockMu.Unlock()
	if c == nil {
		c = SystemClock{}
	}
	clock = c
}

// clockFunc lets a plain function be a [Clock], which is what
// DateFactory::useCallable takes.
type clockFunc func() time.Time

// Now answers to Carbon::now, read from the function.
func (f clockFunc) Now() time.Time { return f() }

// UseCallable answers to DateFactory::useCallable: the function every later
// [Now] reads.
//
// DateFactory::useClass and DateFactory::useFactory take a Carbon subclass name
// and a Carbon factory, neither of which Go has: there is no class string and
// no second date type. The Clock is what is left of the three.
func UseCallable(callable func() time.Time) {
	if callable == nil {
		UseDefault()
		return
	}
	Use(clockFunc(callable))
}

// UseDefault answers to DateFactory::useDefault: back to the system clock, with
// nothing pinned.
func UseDefault() {
	clockMu.Lock()
	defer clockMu.Unlock()
	clock = SystemClock{}
	testNow = nil
}

// Travel answers to travel() of Illuminate\Foundation\Testing\Concerns\
// InteractsWithTime: move the pinned "now" by the given amount and return it.
//
// The PHP returns a Wormhole so the unit can be chained -- travel(5)->days().
// A time.Duration already carries its unit, so the Go call is
// Travel(5 * 24 * time.Hour).
func Travel(d time.Duration) time.Time {
	return TravelTo(Now().Add(d))
}

// TravelTo answers to travelTo(): pin "now" at the given instant and return it.
// With a callback, the PHP runs it and travels back; so does this.
func TravelTo(value time.Time, callback ...func()) time.Time {
	SetTestNow(&value)
	if len(callback) > 0 && callback[0] != nil {
		defer TravelBack()
		callback[0]()
	}
	return value
}

// TravelBack answers to travelBack(): unpin "now".
func TravelBack() { SetTestNow(nil) }

// FreezeTime answers to freezeTime(): pin "now" where it already is, so that
// nothing moves while the callback runs. With no callback it stays frozen until
// [TravelBack].
func FreezeTime(callback ...func()) time.Time {
	return TravelTo(Now(), callback...)
}

// FreezeSecond answers to freezeSecond(): freeze on the start of the current
// second, so a comparison against a whole second holds.
func FreezeSecond(callback ...func()) time.Time {
	return TravelTo(Now().Truncate(time.Second), callback...)
}

// ErrUnknownDelay states the three shapes a delay may take: an instant
// (time.Time), a span (time.Duration) or a plain count of seconds. Anything
// else carries no duration and cannot be turned into one.
//
// [SecondsUntil], [AvailableAt] and [ParseDateInterval] never return it -- they
// take a value they cannot read as zero, so a delay of the wrong type schedules
// work for right now instead of failing. It is here for a caller that has to
// reject such a value rather than fall back, and it is the error to return.
//
// Answers Illuminate\Support\InteractsWithTime.
var ErrUnknownDelay = errors.New("support: delay must be a time.Time, a time.Duration or a number of seconds")

// SecondsUntil answers to InteractsWithTime::secondsUntil: how many seconds
// separate now from the delay, never below zero.
//
// The PHP trait takes DateTimeInterface|DateInterval|int; Go takes the same
// three shapes as time.Time, time.Duration and an integer count of seconds.
func SecondsUntil(delay any) int64 {
	switch d := delay.(type) {
	case time.Time:
		return max64(0, d.Unix()-CurrentTime())
	case *time.Time:
		if d == nil {
			return 0
		}
		return max64(0, d.Unix()-CurrentTime())
	case time.Duration:
		return int64(d / time.Second)
	default:
		return toSeconds(delay)
	}
}

// AvailableAt answers to InteractsWithTime::availableAt: the UNIX timestamp the
// delay lands on.
func AvailableAt(delay any) int64 {
	switch d := delay.(type) {
	case time.Time:
		return d.Unix()
	case *time.Time:
		if d == nil {
			return CurrentTime()
		}
		return d.Unix()
	case time.Duration:
		return Now().Add(d).Unix()
	default:
		return Now().Add(time.Duration(toSeconds(delay)) * time.Second).Unix()
	}
}

// ParseDateInterval answers to InteractsWithTime::parseDateInterval: an
// interval becomes the instant it lands on, everything else is left alone.
func ParseDateInterval(delay any) any {
	if d, ok := delay.(time.Duration); ok {
		return Now().Add(d)
	}
	return delay
}

// CurrentTime answers to InteractsWithTime::currentTime: now as a UNIX
// timestamp.
func CurrentTime() int64 { return Now().Unix() }

// RunTimeForHumans answers to InteractsWithTime::runTimeForHumans: the span
// between the two instants, written the way a console line writes it. Below one
// second it is milliseconds with two decimals; above it, the short cascade
// Carbon prints, as in "1s 250ms".
//
// The PHP takes two float microtimes; Go takes the instants themselves, and an
// absent end is now.
func RunTimeForHumans(start time.Time, end ...time.Time) string {
	finish := Now()
	if len(end) > 0 && !end[0].IsZero() {
		finish = end[0]
	}
	runTime := finish.Sub(start)
	if runTime < 0 {
		runTime = 0
	}
	if runTime <= time.Second {
		return strconv.FormatFloat(float64(runTime)/float64(time.Millisecond), 'f', 2, 64) + "ms"
	}
	return forHumans(runTime)
}

// forHumans is CarbonInterval::cascade()->forHumans(short: true), narrowed to
// the units a run time ever reaches.
func forHumans(d time.Duration) string {
	parts := []string{}
	units := []struct {
		suffix string
		size   time.Duration
	}{
		{"w", 7 * 24 * time.Hour},
		{"d", 24 * time.Hour},
		{"h", time.Hour},
		{"m", time.Minute},
		{"s", time.Second},
		{"ms", time.Millisecond},
	}
	for _, unit := range units {
		if d < unit.size {
			continue
		}
		count := d / unit.size
		d -= count * unit.size
		parts = append(parts, strconv.FormatInt(int64(count), 10)+unit.suffix)
	}
	if len(parts) == 0 {
		return "0ms"
	}
	return strings.Join(parts, " ")
}

// toSeconds reads the numeric shapes PHP would juggle into an int.
func toSeconds(delay any) int64 {
	switch n := delay.(type) {
	case nil:
		return 0
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case float32:
		return int64(n)
	case float64:
		return int64(n)
	case string:
		parsed, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0
		}
		return int64(parsed)
	default:
		return 0
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
