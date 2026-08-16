// Package exceptions holds nothing.
//
// The errors this component returns are declared one directory up, beside the
// functions that return them: process.ProcessFailedException,
// process.ProcessTimedOutException and process.StrayProcessError.
//
// They stayed there because in Go an error type belongs beside the function
// that returns it -- errors.As on the value Run gave back is how a caller
// reaches any of them, and a package boundary between the two would mean an
// import in one direction or the other, for three types and no separation.
package exceptions
