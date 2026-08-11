package collections_test

import (
	"iter"
	"reflect"
	"testing"
	"time"

	"github.com/arandu-io/hesape/collections"
)

// counted returns a lazy collection over 1..n and a pointer to the number of
// elements the source has actually produced, so that a test can prove what was
// pulled and what was not.
func counted(n int) (collections.LazyCollection[int], *int) {
	pulled := 0
	return collections.NewLazyCollection(func(yield func(int, int) bool) {
		for i := 0; i < n; i++ {
			pulled++
			if !yield(i, i+1) {
				return
			}
		}
	}), &pulled
}

func drain[T any](l collections.LazyCollection[T]) []T { return l.All() }

func TestLazyZeroValueYieldsNothing(t *testing.T) {
	var l collections.LazyCollection[int]
	if got := l.All(); got == nil || len(got) != 0 {
		t.Errorf("the zero value must yield nothing and never nil, got %v", got)
	}
	if !l.IsEmpty() {
		t.Error("the zero value is empty")
	}
	if l.Count() != 0 {
		t.Error("the zero value counts zero")
	}
}

func TestLazyIsLazy(t *testing.T) {
	source, pulled := counted(1000)
	first, ok := source.Filter(func(v, _ int) bool { return v%2 == 0 }).First(nil)
	if !ok || first != 2 {
		t.Fatalf("First = (%v, %v)", first, ok)
	}
	if *pulled > 3 {
		t.Errorf("the source produced %d elements; a lazy First must stop at the match", *pulled)
	}
}

func TestLazyTakeStopsTheSource(t *testing.T) {
	source, pulled := counted(1000)
	got := drain(source.Take(3))
	if !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("Take(3) = %v", got)
	}
	if *pulled != 3 {
		t.Errorf("the source produced %d elements, want 3", *pulled)
	}
}

func TestLazyTakeEdges(t *testing.T) {
	source, _ := counted(5)
	if got := drain(source.Take(0)); len(got) != 0 {
		t.Errorf("Take(0) = %v, want nothing", got)
	}
	if got := drain(source.Take(9)); !reflect.DeepEqual(got, []int{1, 2, 3, 4, 5}) {
		t.Errorf("Take past the end = %v", got)
	}
	if got := drain(source.Take(-2)); !reflect.DeepEqual(got, []int{4, 5}) {
		t.Errorf("Take(-2) = %v, want the last two", got)
	}
	short, _ := counted(2)
	if got := drain(short.Take(-3)); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("Take(-3) over two elements = %v, want both in order", got)
	}
}

func TestLazyFilterAndReject(t *testing.T) {
	source, _ := counted(5)
	even := func(v, _ int) bool { return v%2 == 0 }
	if got := drain(source.Filter(even)); !reflect.DeepEqual(got, []int{2, 4}) {
		t.Errorf("Filter = %v", got)
	}
	if got := drain(source.Reject(even)); !reflect.DeepEqual(got, []int{1, 3, 5}) {
		t.Errorf("Reject = %v", got)
	}
	if got := drain(source.Filter(nil)); len(got) != 5 {
		t.Errorf("Filter(nil) = %v, want everything", got)
	}
	never := func(int, int) bool { return false }
	if got := drain(source.Filter(never)); len(got) != 0 {
		t.Errorf("Filter with a callback that never matches = %v", got)
	}
}

func TestLazyFilterRenumbersKeys(t *testing.T) {
	source, _ := counted(4)
	keys := []int{}
	for k := range source.Filter(func(v, _ int) bool { return v%2 == 0 }).GetIterator() {
		keys = append(keys, k)
	}
	if !reflect.DeepEqual(keys, []int{0, 1}) {
		t.Errorf("the survivors must close the gap, got keys %v", keys)
	}
}

func TestMapLazy(t *testing.T) {
	source, _ := counted(3)
	got := MapLazyDrain(collections.MapLazy(source, func(v, _ int) string {
		return string(rune('a' + v - 1))
	}))
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("MapLazy = %v", got)
	}
}

func MapLazyDrain[T any](l collections.LazyCollection[T]) []T { return l.All() }

func TestChunkLazy(t *testing.T) {
	source, _ := counted(5)
	chunks := collections.ChunkLazy(source, 2).All()
	want := []collections.Collection[int]{{1, 2}, {3, 4}, {5}}
	if !reflect.DeepEqual(chunks, want) {
		t.Errorf("ChunkLazy = %v, want %v", chunks, want)
	}
	if got := collections.ChunkLazy(source, 0).All(); len(got) != 0 {
		t.Errorf("ChunkLazy with a size below one = %v, want nothing", got)
	}
}

func TestChunkLazyStaysLazy(t *testing.T) {
	source, pulled := counted(1000)
	first, ok := collections.ChunkLazy(source, 2).First(nil)
	if !ok || !reflect.DeepEqual([]int(first), []int{1, 2}) {
		t.Fatalf("First chunk = (%v, %v)", first, ok)
	}
	if *pulled > 3 {
		t.Errorf("the source produced %d elements for one chunk of two", *pulled)
	}
}

func TestLazyEach(t *testing.T) {
	source, _ := counted(5)
	seen := []int{}
	source.Each(func(v, _ int) bool {
		seen = append(seen, v)
		return v < 3
	})
	if !reflect.DeepEqual(seen, []int{1, 2, 3}) {
		t.Errorf("Each saw %v, want the walk to stop on false", seen)
	}
}

func TestLazyTapEachRunsOnlyWhenWalked(t *testing.T) {
	source, _ := counted(3)
	seen := []int{}
	tapped := source.TapEach(func(v, _ int) { seen = append(seen, v) })
	if len(seen) != 0 {
		t.Fatalf("TapEach must run nothing until the result is walked, saw %v", seen)
	}
	tapped.All()
	if !reflect.DeepEqual(seen, []int{1, 2, 3}) {
		t.Errorf("TapEach saw %v", seen)
	}
}

func TestLazyEagerReadsTheSourceOnce(t *testing.T) {
	source, pulled := counted(3)
	eager := source.Eager()
	if *pulled != 3 {
		t.Errorf("Eager must read the source at once, pulled %d", *pulled)
	}
	eager.All()
	eager.All()
	if *pulled != 3 {
		t.Errorf("Eager must never read the source again, pulled %d", *pulled)
	}
}

func TestLazyRememberMemoizes(t *testing.T) {
	source, pulled := counted(4)
	remembered := source.Remember()

	if *pulled != 0 {
		t.Fatalf("Remember must read nothing up front, pulled %d", *pulled)
	}
	if got := remembered.All(); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Fatalf("the first walk = %v", got)
	}
	afterFirst := *pulled
	if got := remembered.All(); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Fatalf("the second walk = %v", got)
	}
	if *pulled != afterFirst {
		t.Errorf("the second walk pulled %d more from the source; the cache must serve it", *pulled-afterFirst)
	}
}

func TestLazyRememberPullsOnlyPastWhatItReached(t *testing.T) {
	source, pulled := counted(10)
	remembered := source.Remember()

	if got := remembered.Take(2).All(); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("the short walk = %v", got)
	}
	afterShort := *pulled
	if afterShort > 3 {
		t.Errorf("the short walk pulled %d, want no more than it needed", afterShort)
	}
	if got := remembered.Take(4).All(); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Fatalf("the longer walk = %v", got)
	}
	if *pulled-afterShort > 3 {
		t.Errorf("the longer walk pulled %d more; only the part past the cache should come from the source", *pulled-afterShort)
	}
}

func TestLazyRememberSurvivesASingleUseSource(t *testing.T) {
	// A sequence that can only be walked once is exactly what Remember exists
	// for: the second walk must come out of the cache and never touch it again.
	walked := false
	once := collections.NewLazyCollection(func(yield func(int, int) bool) {
		if walked {
			panic("the source was walked twice")
		}
		walked = true
		for i := 0; i < 3; i++ {
			if !yield(i, i+1) {
				return
			}
		}
	})
	remembered := once.Remember()
	first := remembered.All()
	second := remembered.All()
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(first, []int{1, 2, 3}) {
		t.Errorf("the two walks = %v and %v", first, second)
	}
}

func TestLazyRememberOverAnEmptySource(t *testing.T) {
	empty := collections.NewLazyCollection(func(yield func(int, int) bool) {}).Remember()
	if got := empty.All(); len(got) != 0 {
		t.Errorf("the first walk = %v", got)
	}
	if got := empty.All(); len(got) != 0 {
		t.Errorf("the second walk = %v", got)
	}
}

func TestLazySkipAndTakeUntil(t *testing.T) {
	source, _ := counted(5)
	if got := drain(source.Skip(2)); !reflect.DeepEqual(got, []int{3, 4, 5}) {
		t.Errorf("Skip = %v", got)
	}
	if got := drain(source.Skip(99)); len(got) != 0 {
		t.Errorf("Skip past the end = %v", got)
	}
	if got := drain(source.TakeWhile(func(v, _ int) bool { return v < 3 })); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("TakeWhile = %v", got)
	}
	if got := drain(source.TakeUntil(func(v, _ int) bool { return v == 3 })); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("TakeUntil = %v", got)
	}
}

func TestLazyTakeUntilTimeoutAlreadyPast(t *testing.T) {
	source, pulled := counted(5)
	seen := 0
	got := drain(source.TakeUntilTimeout(time.Now().Add(-time.Second), func(int, int) { seen++ }))
	if len(got) != 0 {
		t.Errorf("a deadline already past must produce nothing, got %v", got)
	}
	if *pulled != 0 {
		t.Errorf("the source must not be touched, pulled %d", *pulled)
	}
	if seen != 1 {
		t.Errorf("the callback must run once, ran %d times", seen)
	}
}

func TestRangeLazy(t *testing.T) {
	if got := collections.RangeLazy(1, 3, 1).All(); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("RangeLazy = %v", got)
	}
	if got := collections.RangeLazy(3, 1, 0).All(); !reflect.DeepEqual(got, []int{3, 2, 1}) {
		t.Errorf("RangeLazy counting down with a zero step = %v", got)
	}
}

func TestCollectionLazyRoundTrip(t *testing.T) {
	c := collections.Collect([]int{1, 2, 3})
	if got := c.Lazy().Collect(); !reflect.DeepEqual([]int(got), []int{1, 2, 3}) {
		t.Errorf("Lazy then Collect = %v", got)
	}
	var iterated iter.Seq2[int, int] = c.Lazy().GetIterator()
	total := 0
	for _, v := range iterated {
		total += v
	}
	if total != 6 {
		t.Errorf("ranging the iterator summed %d", total)
	}
}
