package onetime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/onetime"
)

// wrongCode is a code of the right shape that is not code.
func wrongCode(code string) string {
	b := []byte(code)
	b[0] = '0' + (b[0]-'0'+1)%10
	return string(b)
}

// TestIssuingANewCodeDoesNotRestoreTheGuesses: every Issue minted a new nonce
// and with it a new attempt counter, so five guesses a code and one code a
// minute was seven thousand guesses a day against one account -- a reset code
// brute-forced in under five months, and in parallel across accounts. The
// failures are counted per purpose and subject, and a fresh code inherits what
// is left.
func TestIssuingANewCodeDoesNotRestoreTheGuesses(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, newExpiring(clk), clk, onetime.Config{MaxAttempts: 5, MaxFailures: 7})
	ctx := context.Background()

	first, err := codes.Issue(ctx, "reset-password", "person-1")
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if err := codes.Consume(ctx, "reset-password", "person-1", wrongCode(first)); !errors.Is(err, onetime.ErrInvalidCode) {
			t.Fatalf("a wrong guess answered %v, want ErrInvalidCode", err)
		}
	}

	clk.advance(onetime.DefaultCooldown)
	second, err := codes.Issue(ctx, "reset-password", "person-1")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := codes.Consume(ctx, "reset-password", "person-1", wrongCode(second)); !errors.Is(err, onetime.ErrInvalidCode) {
			t.Fatalf("a wrong guess against the new code answered %v, want ErrInvalidCode", err)
		}
	}
	if err := codes.Consume(ctx, "reset-password", "person-1", wrongCode(second)); !errors.Is(err, onetime.ErrTooManyFailures) {
		t.Fatalf("the eighth wrong code across two issues answered %v, want ErrTooManyFailures", err)
	}
	if err := codes.Consume(ctx, "reset-password", "person-1", second); !errors.Is(err, onetime.ErrTooManyFailures) {
		t.Fatalf("the right code past the budget answered %v, want ErrTooManyFailures", err)
	}

	clk.advance(onetime.DefaultCooldown)
	third, err := codes.Issue(ctx, "reset-password", "person-1")
	if err != nil {
		t.Fatalf("issuing is still allowed while the budget is spent: %v", err)
	}
	if err := codes.Consume(ctx, "reset-password", "person-1", third); !errors.Is(err, onetime.ErrTooManyFailures) {
		t.Fatalf("a code issued after the budget was spent answered %v, want ErrTooManyFailures", err)
	}

	clk.advance(onetime.DefaultFailureWindow)
	fourth, err := codes.Issue(ctx, "reset-password", "person-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := codes.Consume(ctx, "reset-password", "person-1", fourth); err != nil {
		t.Fatalf("once the window closed the right code answered %v, want nil", err)
	}
}

// TestTheBudgetIsLimitedByDefault: a zero Config is the one to start from, so
// the budget has to be there without anybody asking for it.
func TestTheBudgetIsLimitedByDefault(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{})
	ctx := context.Background()

	var code string
	for spent := 0; spent < onetime.DefaultMaxFailures; spent++ {
		if spent%onetime.DefaultMaxAttempts == 0 {
			clk.advance(onetime.DefaultCooldown)
			var err error
			if code, err = codes.Issue(ctx, "reset-password", "person-1"); err != nil {
				t.Fatal(err)
			}
		}
		if err := codes.Consume(ctx, "reset-password", "person-1", wrongCode(code)); !errors.Is(err, onetime.ErrInvalidCode) {
			t.Fatalf("wrong guess %d answered %v, want ErrInvalidCode", spent+1, err)
		}
	}
	clk.advance(onetime.DefaultCooldown)
	code, err := codes.Issue(ctx, "reset-password", "person-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := codes.Consume(ctx, "reset-password", "person-1", code); !errors.Is(err, onetime.ErrTooManyFailures) {
		t.Fatalf("after %d wrong codes the right one answered %v, want ErrTooManyFailures", onetime.DefaultMaxFailures, err)
	}
	if err := codes.Consume(ctx, "reset-password", "person-2", code); errors.Is(err, onetime.ErrTooManyFailures) {
		t.Fatal("one subject's spent budget refused another subject")
	}
}

// TestARightCodeIsNotAFailure: the budget is counted before the comparison,
// so the right code has to take its own count back, or every successful use
// would spend the budget of a person who never typed a wrong digit.
func TestARightCodeIsNotAFailure(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{MaxFailures: 2})
	ctx := context.Background()

	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", wrongCode(code)); !errors.Is(err, onetime.ErrInvalidCode) {
		t.Fatal(err)
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", code); err != nil {
		t.Fatalf("the right code answered %v", err)
	}
	clk.advance(time.Second)
	code, err = codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", wrongCode(code)); !errors.Is(err, onetime.ErrInvalidCode) {
		t.Fatalf("the second wrong code answered %v, want ErrInvalidCode: the right one was counted as a failure", err)
	}
}

func TestANegativeFailureBudgetIsRefused(t *testing.T) {
	for _, cfg := range []onetime.Config{{MaxFailures: -1}, {FailureWindow: -time.Second}} {
		if _, err := onetime.New(cache.NewArrayStore(), appKey, cfg); err == nil {
			t.Errorf("New accepted %+v", cfg)
		}
	}
}
