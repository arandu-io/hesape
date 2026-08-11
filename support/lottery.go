package support

import (
	"crypto/rand"
	"errors"
	"math"
	"math/big"
	"sync"
)

// ErrFloatGreaterThanOne answers to the RuntimeException Lottery::__construct
// raises for odds above one with no total to be out of.
//
// The message is the PHP one, capital and full stop included.
var ErrFloatGreaterThanOne = errors.New("Float must not be greater than 1.")

// Lottery answers to Illuminate\Support\Lottery: run one callback or the other,
// at the odds given, so that a slow path runs on a fraction of the requests.
type Lottery struct {
	chances float64
	outOf   *int
	winner  func() any
	loser   func() any
}

var (
	lotteryMu            sync.Mutex
	lotteryResultFactory func(chances float64, outOf *int) bool
)

// NewLottery answers to Lottery::__construct. The variadic argument stands for
// the PHP $outOf, which is null.
//
// The PHP rejects a float above one only because an int above one is a whole
// number of chances; Go has one numeric type here, so any odds above one with
// no total is [ErrFloatGreaterThanOne].
func NewLottery(chances float64, outOf ...int) (*Lottery, error) {
	l := &Lottery{chances: chances}
	if len(outOf) > 0 {
		total := outOf[0]
		l.outOf = &total
		return l, nil
	}
	if chances > 1 {
		return nil, ErrFloatGreaterThanOne
	}
	return l, nil
}

// Odds answers to Lottery::odds.
func Odds(chances float64, outOf ...int) (*Lottery, error) {
	return NewLottery(chances, outOf...)
}

// Winner answers to Lottery::winner.
func (l *Lottery) Winner(callback func() any) *Lottery {
	l.winner = callback
	return l
}

// Loser answers to Lottery::loser.
func (l *Lottery) Loser(callback func() any) *Lottery {
	l.loser = callback
	return l
}

// Choose answers to Lottery::choose. With no argument it runs the lottery once
// and hands back what the callback returned; with a count it hands back a []any
// of that many results, which is the PHP's own pair of return shapes.
func (l *Lottery) Choose(times ...int) any {
	if len(times) == 0 {
		return l.runCallback()
	}
	results := []any{}
	for i := 0; i < times[0]; i++ {
		results = append(results, l.runCallback())
	}
	return results
}

// runCallback answers to the protected Lottery::runCallback. A lottery with no
// callback of its own answers true when it wins and false when it loses, which
// is what the PHP fn () => true and fn () => false do.
func (l *Lottery) runCallback() any {
	if l.wins() {
		if l.winner == nil {
			return true
		}
		return l.winner()
	}
	if l.loser == nil {
		return false
	}
	return l.loser()
}

// wins answers to the protected Lottery::wins.
func (l *Lottery) wins() bool { return resultFactory()(l.chances, l.outOf) }

// resultFactory answers to the protected static Lottery::resultFactory.
func resultFactory() func(chances float64, outOf *int) bool {
	lotteryMu.Lock()
	factory := lotteryResultFactory
	lotteryMu.Unlock()
	if factory != nil {
		return factory
	}
	return defaultResultFactory
}

// defaultResultFactory is the closure the PHP falls back to: with no total, a
// random number in [0, 1] under the odds; with one, a draw from 1 to the total
// that lands on or under the chances.
func defaultResultFactory(chances float64, outOf *int) bool {
	if outOf == nil {
		return randomFloat() <= chances
	}
	if *outOf <= 0 {
		return false
	}
	return float64(randomInt(1, *outOf)) <= chances
}

// randomFloat is random_int(0, PHP_INT_MAX) / PHP_INT_MAX, drawn from the
// cryptographic source because random_int is one.
func randomFloat() float64 {
	drawn, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		return 0
	}
	return float64(drawn.Int64()) / float64(math.MaxInt64)
}

// randomInt is random_int(min, max), both ends included.
func randomInt(minimum, maximum int) int {
	if maximum <= minimum {
		return minimum
	}
	drawn, err := rand.Int(rand.Reader, big.NewInt(int64(maximum-minimum+1)))
	if err != nil {
		return minimum
	}
	return minimum + int(drawn.Int64())
}

// AlwaysWin answers to Lottery::alwaysWin. The variadic argument stands for the
// PHP $callback, which is null; with one, the lottery is put back to normal
// after it has run.
func AlwaysWin(callback ...func()) {
	SetResultFactory(func(float64, *int) bool { return true })
	if len(callback) == 0 || callback[0] == nil {
		return
	}
	callback[0]()
	DetermineResultNormally()
}

// AlwaysLose answers to Lottery::alwaysLose.
func AlwaysLose(callback ...func()) {
	SetResultFactory(func(float64, *int) bool { return false })
	if len(callback) == 0 || callback[0] == nil {
		return
	}
	callback[0]()
	DetermineResultNormally()
}

// Fix answers to Lottery::fix.
func Fix(sequence []bool, whenMissing ...func(chances float64, outOf *int) bool) {
	ForceResultWithSequence(sequence, whenMissing...)
}

// ForceResultWithSequence answers to Lottery::forceResultWithSequence: the
// results the lottery gives, in order, and what to do once they run out.
//
// The variadic argument stands for the PHP $whenMissing, which is null and
// means "draw normally from there on".
func ForceResultWithSequence(sequence []bool, whenMissing ...func(chances float64, outOf *int) bool) {
	var mu sync.Mutex
	next := 0

	missing := firstOr(whenMissing, nil)
	if missing == nil {
		missing = func(chances float64, outOf *int) bool {
			result := defaultResultFactory(chances, outOf)
			next++
			return result
		}
	}

	SetResultFactory(func(chances float64, outOf *int) bool {
		mu.Lock()
		defer mu.Unlock()
		if next < len(sequence) {
			result := sequence[next]
			next++
			return result
		}
		return missing(chances, outOf)
	})
}

// DetermineResultsNormally answers to Lottery::determineResultsNormally.
func DetermineResultsNormally() { DetermineResultNormally() }

// DetermineResultNormally answers to Lottery::determineResultNormally.
func DetermineResultNormally() {
	lotteryMu.Lock()
	defer lotteryMu.Unlock()
	lotteryResultFactory = nil
}

// SetResultFactory answers to Lottery::setResultFactory.
func SetResultFactory(factory func(chances float64, outOf *int) bool) {
	lotteryMu.Lock()
	defer lotteryMu.Unlock()
	lotteryResultFactory = factory
}
