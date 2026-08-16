// Package console is the command-line surface of hesape/concurrency.
//
// It is empty, and it stays empty. The tasks that package runs are goroutines
// in the current process, so there is no second process to hand work to and no
// command for that process to run on the far side.
//
// The package comment of hesape/concurrency says why the three driver names
// resolve to a single driver.
package console
