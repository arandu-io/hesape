// Package exceptions mirrors Illuminate\Process\Exceptions, and holds nothing.
//
// The files it answers to, in the clone at
// laravel_illuminate/process/Exceptions:
//
//	ProcessFailedException.php
//	ProcessTimedOutException.php
//
// Both are implemented one directory up, under their own names:
// process.ProcessFailedException and process.ProcessTimedOutException. Beside
// them is process.StrayProcessError, which Illuminate raises as a plain
// RuntimeException from preventStrayProcesses and which is a named type here so
// that errors.As can tell it from a program that really failed.
//
// They stayed there because in Go an error type belongs beside the function
// that returns it -- errors.As on the value Run gave back is how a caller
// reaches either, and a package boundary between the two would mean either an
// import of the parent from here or of here from the parent, for two types and
// no separation. PHP puts them in a sub-namespace because a namespace is free;
// a Go package is not.
package exceptions
