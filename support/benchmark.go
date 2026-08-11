package support

import (
	"runtime"
	"strconv"
	"time"
)

// benchmarkFacade is Illuminate\Support\Benchmark. It is reached through the
// [Benchmark] value rather than a type because every method of the PHP class is
// static, the way [Env] is.
type benchmarkFacade struct{}

// Benchmark answers to Illuminate\Support\Benchmark: how long a callback takes,
// in milliseconds. support.Benchmark.Measure(...) reads as Benchmark::measure.
var Benchmark benchmarkFacade

// Measure answers to Benchmark::measure: the average run of each callback, in
// milliseconds, over the given number of iterations.
//
// The PHP takes a Closure or an array of them and returns a float or an array
// to match; Go has one return type, so a one-callback call reads results[0].
// The variadic argument stands for the PHP $iterations, whose default is 1.
func (benchmarkFacade) Measure(benchmarkables []func(), iterations ...int) []float64 {
	count := firstOr(iterations, 1)
	results := make([]float64, 0, len(benchmarkables))
	for _, callback := range benchmarkables {
		if count < 1 {
			results = append(results, 0)
			continue
		}
		total := float64(0)
		for i := 0; i < count; i++ {
			runtime.GC()
			start := time.Now()
			callback()
			total += float64(time.Since(start)) / float64(time.Millisecond)
		}
		results = append(results, total/float64(count))
	}
	return results
}

// Value answers to Benchmark::value: what the callback returned and how long it
// took, in milliseconds.
//
// The PHP is generic over mixed; this is generic over T, so the value comes
// back with the type it went in with.
func Value[T any](callback func() T) (T, float64) {
	runtime.GC()
	start := time.Now()
	result := callback()
	return result, float64(time.Since(start)) / float64(time.Millisecond)
}

// Dd answers to Benchmark::dd: measure, write the averages out with three
// decimals and the ms suffix, then end the process.
func (b benchmarkFacade) Dd(benchmarkables []func(), iterations ...int) {
	measured := b.Measure(benchmarkables, iterations...)
	written := make([]string, 0, len(measured))
	for _, average := range measured {
		written = append(written, strconv.FormatFloat(average, 'f', 3, 64)+"ms")
	}
	Dd(written)
}
