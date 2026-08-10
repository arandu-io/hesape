//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package console

import "os"

// A system without a termios and without a Windows console. js/wasm and plan9
// are what this is, and neither has a terminal to ask.
//
// Saying so is the point: Secret refuses rather than reading a password that
// the screen would then be holding.

// isTerminal reports whether f is a terminal, and here it never is.
func isTerminal(f *os.File) bool { return false }

// hideEcho cannot turn the echo off on this system, and says so.
func hideEcho(f *os.File) (func(), bool) { return nil, false }
