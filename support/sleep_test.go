package support_test

import (
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/support"
)

func TestForBuildsTheDurationOutOfEveryUnitItIsGiven(t *testing.T) {
	slept := support.For(1).Second().And(500).Milliseconds()

	if slept.Duration != time.Second+500*time.Millisecond {
		t.Fatalf("got %s, want 1.5s", slept.Duration)
	}
}

func TestForTakesADurationWholeAndClampsAPastOneToNothing(t *testing.T) {
	if got := support.For(2 * time.Second).Duration; got != 2*time.Second {
		t.Fatalf("got %s, want 2s", got)
	}
	if got := support.For(-2 * time.Second).Duration; got != 0 {
		t.Fatalf("a negative interval is no sleep at all, got %s", got)
	}
}

func TestANegativeNumberOfSecondsIsNoSleepAtAll(t *testing.T) {
	if got := support.For(-5).Seconds().Duration; got != 0 {
		t.Fatalf("got %s, want 0", got)
	}
}

func TestAUnitWithNothingPendingIsAnError(t *testing.T) {
	slept := support.For(1).Second().Seconds()

	if err := slept.Goodnight(); !errors.Is(err, support.ErrNoDurationSpecified) {
		t.Fatalf("got %v, want ErrNoDurationSpecified", err)
	}
}

func TestANumberWithNoUnitIsAnError(t *testing.T) {
	support.Fake()
	defer support.Fake(false)

	if err := support.For(1).Goodnight(); !errors.Is(err, support.ErrUnknownDurationUnit) {
		t.Fatalf("got %v, want ErrUnknownDurationUnit", err)
	}
}

func TestFakeCapturesTheSleepInsteadOfServingIt(t *testing.T) {
	support.Fake()
	defer support.Fake(false)

	started := time.Now()
	if err := support.For(5).Seconds().Goodnight(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("the fake slept for real")
	}

	support.AssertSleptTimes(t, 1)
	support.AssertSlept(t, func(d time.Duration) bool { return d == 5*time.Second })
}

func TestAssertNeverSleptAndAssertInsomniac(t *testing.T) {
	support.Fake()
	defer support.Fake(false)

	support.AssertNeverSlept(t)

	if err := support.For(0).Seconds().Goodnight(); err != nil {
		t.Fatal(err)
	}
	support.AssertInsomniac(t)
	support.AssertSleptTimes(t, 1)
}

func TestAssertSequenceComparesEachSleepAndSkipsANilOne(t *testing.T) {
	support.Fake()
	defer support.Fake(false)

	_ = support.For(1).Second().Goodnight()
	_ = support.For(2).Seconds().Goodnight()

	support.AssertSequence(t, []*support.Sleep{support.For(1).Second(), nil})
}

func TestUsleepIsMicroseconds(t *testing.T) {
	if got := support.Usleep(1500).Duration; got != 1500*time.Microsecond {
		t.Fatalf("got %s, want 1.5ms", got)
	}
}

func TestUntilAnInstantAlreadyPastIsNoSleepAtAll(t *testing.T) {
	pinned := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	support.SetTestNow(&pinned)
	defer support.TravelBack()

	if got := support.Until(pinned.Add(30 * time.Second)).Duration; got != 30*time.Second {
		t.Fatalf("got %s, want 30s", got)
	}
	if got := support.Until(pinned.Add(-30 * time.Second)).Duration; got != 0 {
		t.Fatalf("got %s, want 0", got)
	}
}

func TestWhenAndUnlessDecideWhetherTheSleepHappensAtAll(t *testing.T) {
	support.Fake()
	defer support.Fake(false)

	_ = support.For(1).Second().When(false).Goodnight()
	support.AssertNeverSlept(t)

	_ = support.For(1).Second().Unless(func(s *support.Sleep) bool { return s.Duration > 0 }).Goodnight()
	support.AssertNeverSlept(t)

	_ = support.For(1).Second().When(true).Goodnight()
	support.AssertSleptTimes(t, 1)
}

func TestThenRunsTheCallbackAfterTheSleep(t *testing.T) {
	support.Fake()
	defer support.Fake(false)

	ran := false
	got, err := support.For(1).Second().Then(func() any {
		ran = true
		return "done"
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ran || got != "done" {
		t.Fatalf("got %v, ran %v", got, ran)
	}
	support.AssertSleptTimes(t, 1)
}

func TestGoodnightSleepsOnceHoweverOftenItIsCalled(t *testing.T) {
	support.Fake()
	defer support.Fake(false)

	slept := support.For(1).Second()
	_ = slept.Goodnight()
	_ = slept.Goodnight()

	support.AssertSleptTimes(t, 1)
}

func TestSyncWithCarbonMovesTheClockForwardByEverySleep(t *testing.T) {
	pinned := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	support.SetTestNow(&pinned)
	defer support.TravelBack()

	support.Fake(true, true)
	defer support.Fake(false)

	_ = support.For(90).Seconds().Goodnight()

	if got := support.Now(); !got.Equal(pinned.Add(90 * time.Second)) {
		t.Fatalf("got %s, want %s", got, pinned.Add(90*time.Second))
	}
}

func TestWhenFakingSleepSeesEveryCapturedDuration(t *testing.T) {
	support.Fake()
	defer support.Fake(false)

	seen := []time.Duration{}
	support.WhenFakingSleep(func(d time.Duration) { seen = append(seen, d) })

	_ = support.For(1).Second().Goodnight()
	_ = support.For(250).Milliseconds().Goodnight()

	if len(seen) != 2 || seen[0] != time.Second || seen[1] != 250*time.Millisecond {
		t.Fatalf("got %v", seen)
	}
}

func TestTheTimeboxWaitsThroughSleepSoAFakeCapturesIt(t *testing.T) {
	support.Fake()
	defer support.Fake(false)

	started := time.Now()
	got, err := support.NewTimebox().Call(func(*support.Timebox) (any, error) { return "user", nil }, 2_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if got != "user" {
		t.Fatalf("got %v, want user", got)
	}
	if time.Since(started) > time.Second {
		t.Fatal("the timebox waited for real while sleeping was faked")
	}
	support.AssertSleptTimes(t, 1)
}
