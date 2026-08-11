package support

import (
	"errors"
	"testing"
	"time"
)

func TestForBuildsTheDurationOutOfEveryUnitItIsGiven(t *testing.T) {
	slept := For(1).Second().And(500).Milliseconds()

	if slept.Duration != time.Second+500*time.Millisecond {
		t.Fatalf("got %s, want 1.5s", slept.Duration)
	}
}

func TestForTakesADurationWholeAndClampsAPastOneToNothing(t *testing.T) {
	if got := For(2 * time.Second).Duration; got != 2*time.Second {
		t.Fatalf("got %s, want 2s", got)
	}
	if got := For(-2 * time.Second).Duration; got != 0 {
		t.Fatalf("a negative interval is no sleep at all, got %s", got)
	}
}

func TestANegativeNumberOfSecondsIsNoSleepAtAll(t *testing.T) {
	if got := For(-5).Seconds().Duration; got != 0 {
		t.Fatalf("got %s, want 0", got)
	}
}

func TestAUnitWithNothingPendingIsAnError(t *testing.T) {
	slept := For(1).Second().Seconds()

	if err := slept.Goodnight(); !errors.Is(err, ErrNoDurationSpecified) {
		t.Fatalf("got %v, want ErrNoDurationSpecified", err)
	}
}

func TestANumberWithNoUnitIsAnError(t *testing.T) {
	Fake()
	defer Fake(false)

	if err := For(1).Goodnight(); !errors.Is(err, ErrUnknownDurationUnit) {
		t.Fatalf("got %v, want ErrUnknownDurationUnit", err)
	}
}

func TestFakeCapturesTheSleepInsteadOfServingIt(t *testing.T) {
	Fake()
	defer Fake(false)

	started := time.Now()
	if err := For(5).Seconds().Goodnight(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("the fake slept for real")
	}

	AssertSleptTimes(t, 1)
	AssertSlept(t, func(d time.Duration) bool { return d == 5*time.Second })
}

func TestAssertNeverSleptAndAssertInsomniac(t *testing.T) {
	Fake()
	defer Fake(false)

	AssertNeverSlept(t)

	if err := For(0).Seconds().Goodnight(); err != nil {
		t.Fatal(err)
	}
	AssertInsomniac(t)
	AssertSleptTimes(t, 1)
}

func TestAssertSequenceComparesEachSleepAndSkipsANilOne(t *testing.T) {
	Fake()
	defer Fake(false)

	_ = For(1).Second().Goodnight()
	_ = For(2).Seconds().Goodnight()

	AssertSequence(t, []*Sleep{For(1).Second(), nil})
}

func TestUsleepIsMicroseconds(t *testing.T) {
	if got := Usleep(1500).Duration; got != 1500*time.Microsecond {
		t.Fatalf("got %s, want 1.5ms", got)
	}
}

func TestUntilAnInstantAlreadyPastIsNoSleepAtAll(t *testing.T) {
	pinned := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	SetTestNow(&pinned)
	defer TravelBack()

	if got := Until(pinned.Add(30 * time.Second)).Duration; got != 30*time.Second {
		t.Fatalf("got %s, want 30s", got)
	}
	if got := Until(pinned.Add(-30 * time.Second)).Duration; got != 0 {
		t.Fatalf("got %s, want 0", got)
	}
}

func TestWhenAndUnlessDecideWhetherTheSleepHappensAtAll(t *testing.T) {
	Fake()
	defer Fake(false)

	_ = For(1).Second().When(false).Goodnight()
	AssertNeverSlept(t)

	_ = For(1).Second().Unless(func(s *Sleep) bool { return s.Duration > 0 }).Goodnight()
	AssertNeverSlept(t)

	_ = For(1).Second().When(true).Goodnight()
	AssertSleptTimes(t, 1)
}

func TestThenRunsTheCallbackAfterTheSleep(t *testing.T) {
	Fake()
	defer Fake(false)

	ran := false
	got, err := For(1).Second().Then(func() any {
		ran = true
		return "done"
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ran || got != "done" {
		t.Fatalf("got %v, ran %v", got, ran)
	}
	AssertSleptTimes(t, 1)
}

func TestGoodnightSleepsOnceHoweverOftenItIsCalled(t *testing.T) {
	Fake()
	defer Fake(false)

	slept := For(1).Second()
	_ = slept.Goodnight()
	_ = slept.Goodnight()

	AssertSleptTimes(t, 1)
}

func TestSyncWithCarbonMovesTheClockForwardByEverySleep(t *testing.T) {
	pinned := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	SetTestNow(&pinned)
	defer TravelBack()

	Fake(true, true)
	defer Fake(false)

	_ = For(90).Seconds().Goodnight()

	if got := Now(); !got.Equal(pinned.Add(90 * time.Second)) {
		t.Fatalf("got %s, want %s", got, pinned.Add(90*time.Second))
	}
}

func TestWhenFakingSleepSeesEveryCapturedDuration(t *testing.T) {
	Fake()
	defer Fake(false)

	seen := []time.Duration{}
	WhenFakingSleep(func(d time.Duration) { seen = append(seen, d) })

	_ = For(1).Second().Goodnight()
	_ = For(250).Milliseconds().Goodnight()

	if len(seen) != 2 || seen[0] != time.Second || seen[1] != 250*time.Millisecond {
		t.Fatalf("got %v", seen)
	}
}

func TestTheTimeboxWaitsThroughSleepSoAFakeCapturesIt(t *testing.T) {
	Fake()
	defer Fake(false)

	started := time.Now()
	got, err := NewTimebox().Call(func(*Timebox) (any, error) { return "user", nil }, 2_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if got != "user" {
		t.Fatalf("got %v, want user", got)
	}
	if time.Since(started) > time.Second {
		t.Fatal("the timebox waited for real while sleeping was faked")
	}
	AssertSleptTimes(t, 1)
}
