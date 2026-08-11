package support

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// ErrNoDurationSpecified answers to the RuntimeException Sleep::pullPending
// raises when a unit is asked for and no number is pending.
//
// The message is the PHP one, capital and full stop included, because it is
// what a person searching for the failure has read before.
var ErrNoDurationSpecified = errors.New("No duration specified.")

// ErrUnknownDurationUnit answers to the RuntimeException Sleep::goodnight
// raises when a number was given and no unit ever followed it.
var ErrUnknownDurationUnit = errors.New("Unknown duration unit.")

// Sleep answers to Illuminate\Support\Sleep: a duration built up unit by unit,
// which a test can capture instead of waiting out.
//
// The PHP sleeps in __destruct, so the object never has to be told to. Go has
// no destructor, so the sleep happens when [Sleep.Goodnight] -- the name of the
// method the PHP __destruct delegates to -- or [Sleep.Then] is called. That is
// the one difference; every other name is the PHP one.
type Sleep struct {
	// Duration answers to the public $duration property. The PHP holds a
	// CarbonInterval; a time.Duration already carries its unit.
	Duration time.Duration

	// while answers to the public $while property. It is not a field named
	// While because [Sleep.While] already is, and Go has one namespace for
	// the two.
	while func() bool

	pending      *float64
	shouldSleep  bool
	alreadySlept bool
	err          error
}

var (
	sleepMu             sync.Mutex
	sleepFake           bool
	sleepSequence       []time.Duration
	sleepFakeCallbacks  []func(duration time.Duration)
	sleepSyncWithCarbon bool
)

// newSleep answers to Sleep::__construct.
func newSleep(duration any) *Sleep {
	s := &Sleep{shouldSleep: true}
	return s.duration(duration)
}

// For answers to Sleep::for. The PHP takes a DateInterval or a number; here a
// time.Duration is the interval and any other number is pending until a unit
// method names it.
//
// Sleep::sleep is this with the unit already named -- Sleep::sleep(2) is
// For(2).Seconds() -- and it does not get a name of its own here, because the
// type holds it.
func For(duration any) *Sleep { return newSleep(duration) }

// Until answers to Sleep::until: sleep until the given instant. The PHP also
// accepts a numeric timestamp, and so does this: an integer is read as seconds
// since the epoch. An instant already past is no sleep at all.
func Until(timestamp any) *Sleep {
	var target time.Time
	switch t := timestamp.(type) {
	case time.Time:
		target = t
	case *time.Time:
		if t != nil {
			target = *t
		}
	default:
		target = CreateFromTimestamp(toSeconds(timestamp))
	}
	return newSleep(target.Sub(Now()))
}

// Usleep answers to Sleep::usleep: the given number of microseconds.
func Usleep(duration int) *Sleep { return newSleep(duration).Microseconds() }

// duration answers to the protected Sleep::duration. A negative interval is
// clamped to nothing, which is what the totalMicroseconds check does.
func (s *Sleep) duration(duration any) *Sleep {
	if d, ok := duration.(time.Duration); ok {
		if d < 0 {
			d = 0
		}
		s.Duration = d
		s.pending = nil
		return s
	}
	s.Duration = 0
	pending := float64(0)
	switch n := duration.(type) {
	case int:
		pending = float64(n)
	case int64:
		pending = float64(n)
	case float32:
		pending = float64(n)
	case float64:
		pending = n
	default:
		pending = toFloat(duration)
	}
	s.pending = &pending
	return s
}

// pullPending answers to the protected Sleep::pullPending. The PHP throws; the
// error is held on the instance and handed back by [Sleep.Goodnight], because a
// fluent call cannot return one.
func (s *Sleep) pullPending() float64 {
	if s.pending == nil {
		s.shouldNotSleep()
		if s.err == nil {
			s.err = ErrNoDurationSpecified
		}
		return 0
	}
	pending := *s.pending
	if pending < 0 {
		pending = 0
	}
	s.pending = nil
	return pending
}

func (s *Sleep) add(unit time.Duration) *Sleep {
	s.Duration += time.Duration(s.pullPending() * float64(unit))
	return s
}

// Minutes answers to Sleep::minutes.
func (s *Sleep) Minutes() *Sleep { return s.add(time.Minute) }

// Minute answers to Sleep::minute.
func (s *Sleep) Minute() *Sleep { return s.Minutes() }

// Seconds answers to Sleep::seconds.
func (s *Sleep) Seconds() *Sleep { return s.add(time.Second) }

// Second answers to Sleep::second.
func (s *Sleep) Second() *Sleep { return s.Seconds() }

// Milliseconds answers to Sleep::milliseconds.
func (s *Sleep) Milliseconds() *Sleep { return s.add(time.Millisecond) }

// Millisecond answers to Sleep::millisecond.
func (s *Sleep) Millisecond() *Sleep { return s.Milliseconds() }

// Microseconds answers to Sleep::microseconds.
func (s *Sleep) Microseconds() *Sleep { return s.add(time.Microsecond) }

// Microsecond answers to Sleep::microsecond.
func (s *Sleep) Microsecond() *Sleep { return s.Microseconds() }

// And answers to Sleep::and: another number, waiting for its unit.
func (s *Sleep) And(duration any) *Sleep {
	pending := toFloat(duration)
	s.pending = &pending
	return s
}

// While answers to Sleep::while: keep sleeping while the callback says so.
func (s *Sleep) While(callback func() bool) *Sleep {
	s.while = callback
	return s
}

// When answers to Sleep::when: sleep only when the condition holds. The PHP
// takes a bool or a Closure receiving the instance, and so does this.
func (s *Sleep) When(condition any) *Sleep {
	switch c := condition.(type) {
	case bool:
		s.shouldSleep = c
	case func() bool:
		s.shouldSleep = c()
	case func(*Sleep) bool:
		s.shouldSleep = c(s)
	default:
		s.shouldSleep = toBool(condition)
	}
	return s
}

// Unless answers to Sleep::unless.
func (s *Sleep) Unless(condition any) *Sleep {
	switch c := condition.(type) {
	case bool:
		return s.When(!c)
	case func() bool:
		return s.When(!c())
	case func(*Sleep) bool:
		return s.When(!c(s))
	default:
		return s.When(!toBool(condition))
	}
}

// shouldNotSleep answers to the protected Sleep::shouldNotSleep.
func (s *Sleep) shouldNotSleep() *Sleep {
	s.shouldSleep = false
	return s
}

// Then answers to Sleep::then: sleep, then run the callback and hand back what
// it returned. The PHP throws out of the sleep, so this returns (any, error)
// and does not run the callback when the sleep failed.
func (s *Sleep) Then(then func() any) (any, error) {
	if err := s.Goodnight(); err != nil {
		return nil, err
	}
	s.alreadySlept = true
	return then(), nil
}

// Goodnight answers to the protected Sleep::goodnight, which the PHP calls from
// __destruct. Go has no destructor, so this is where the sleep happens, and it
// is what a Go caller writes at the end of the chain.
//
// Calling it twice sleeps once, the way the PHP alreadySlept flag arranges.
func (s *Sleep) Goodnight() error {
	if s.alreadySlept || !s.shouldSleep {
		return s.err
	}
	if s.err != nil {
		return s.err
	}
	if s.pending != nil {
		return ErrUnknownDurationUnit
	}
	s.alreadySlept = true

	sleepMu.Lock()
	faking := sleepFake
	syncWithCarbon := sleepSyncWithCarbon
	if faking {
		sleepSequence = append(sleepSequence, s.Duration)
	}
	callbacks := make([]func(time.Duration), len(sleepFakeCallbacks))
	copy(callbacks, sleepFakeCallbacks)
	sleepMu.Unlock()

	if faking {
		if syncWithCarbon {
			moved := Now().Add(s.Duration)
			SetTestNow(&moved)
		}
		for _, callback := range callbacks {
			callback(s.Duration)
		}
		return nil
	}

	remaining := s.Duration
	seconds := remaining / time.Second * time.Second

	while := s.while
	if while == nil {
		calls := 0
		while = func() bool {
			calls++
			return calls == 1
		}
	}

	for while() {
		if seconds > 0 {
			time.Sleep(seconds)
			remaining -= seconds
		}
		if remaining > 0 {
			time.Sleep(remaining)
		}
	}
	return nil
}

// Fake answers to Sleep::fake: stay awake and record what would have been
// slept. The variadic argument stands for the PHP $value and $syncWithCarbon,
// in that order, whose defaults are true and false.
func Fake(options ...bool) {
	sleepMu.Lock()
	defer sleepMu.Unlock()
	sleepFake = true
	if len(options) > 0 {
		sleepFake = options[0]
	}
	sleepSyncWithCarbon = len(options) > 1 && options[1]
	sleepSequence = nil
	sleepFakeCallbacks = nil
}

// SyncWithCarbon answers to Sleep::syncWithCarbon: while faking, move the
// pinned "now" forward by every sleep. The variadic argument stands for the PHP
// $value, whose default is true.
func SyncWithCarbon(value ...bool) {
	sleepMu.Lock()
	defer sleepMu.Unlock()
	sleepSyncWithCarbon = firstOr(value, true)
}

// WhenFakingSleep answers to Sleep::whenFakingSleep: a callback run with every
// duration that was captured instead of slept.
func WhenFakingSleep(callback func(duration time.Duration)) {
	sleepMu.Lock()
	defer sleepMu.Unlock()
	sleepFakeCallbacks = append(sleepFakeCallbacks, callback)
}

// sleepRecorded is the protected static $sequence, copied out under the lock.
func sleepRecorded() []time.Duration {
	sleepMu.Lock()
	defer sleepMu.Unlock()
	out := make([]time.Duration, len(sleepSequence))
	copy(out, sleepSequence)
	return out
}

// AssertSlept answers to Sleep::assertSlept. PHPUnit's static Assert has no Go
// counterpart, so the testing.TB is the first argument, as it is everywhere in
// Go. The variadic argument stands for the PHP $times, whose default is 1.
func AssertSlept(t testing.TB, expected func(duration time.Duration) bool, times ...int) {
	t.Helper()
	want := firstOr(times, 1)
	count := 0
	for _, duration := range sleepRecorded() {
		if expected(duration) {
			count++
		}
	}
	if count != want {
		t.Errorf("The expected sleep was found [%d] times instead of [%d].", count, want)
	}
}

// AssertSleptTimes answers to Sleep::assertSleptTimes.
func AssertSleptTimes(t testing.TB, expected int) {
	t.Helper()
	count := len(sleepRecorded())
	if count != expected {
		t.Errorf("Expected [%d] sleeps but found [%d].", expected, count)
	}
}

// AssertSequence answers to Sleep::assertSequence. A nil entry stands for the
// PHP null, which skips the comparison at that position.
func AssertSequence(t testing.TB, sequence []*Sleep) {
	t.Helper()
	AssertSleptTimes(t, len(sequence))
	recorded := sleepRecorded()
	for i, expected := range sequence {
		if expected == nil || i >= len(recorded) {
			continue
		}
		expected.shouldNotSleep()
		if expected.Duration != recorded[i] {
			t.Errorf("Expected sleep duration of [%s] but actually slept for [%s].",
				expected.Duration, recorded[i])
		}
	}
}

// AssertNeverSlept answers to Sleep::assertNeverSlept.
func AssertNeverSlept(t testing.TB) {
	t.Helper()
	AssertSleptTimes(t, 0)
}

// AssertInsomniac answers to Sleep::assertInsomniac: sleeping was asked for,
// but every duration was zero.
func AssertInsomniac(t testing.TB) {
	t.Helper()
	for _, duration := range sleepRecorded() {
		if duration != 0 {
			t.Errorf("Unexpected sleep duration of [%s] found.", duration)
		}
	}
}
