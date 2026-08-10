package process

import "time"

// Result is what happened when a command ran.
//
// It is filled whether the command succeeded or not, so a caller that only
// wanted the output does not have to take it back out of an error. What it does
// not carry is the Command: Stdin and Env are the two places a secret is passed
// to a program, and a Result is the thing that gets logged.
type Result struct {
	// Name and Args are the program that ran, repeated here so the Result says
	// what it is about on its own.
	Name string
	Args []string
	// ExitCode is what the program returned. It is -1 when the program was
	// killed by a signal -- which is what a timeout and a cancelled context
	// both look like -- and when it never started at all.
	ExitCode int
	// Stdout and Stderr are what the program wrote, capped by Command.MaxOutput.
	Stdout string
	Stderr string
	// Duration is from the moment the program was started to the moment it was
	// reaped.
	Duration time.Duration
	// Truncated reports that Command.MaxOutput dropped part of one of the
	// streams. Without it, a cap turns a long build log into a short one that
	// says nothing about being short.
	Truncated bool
}
