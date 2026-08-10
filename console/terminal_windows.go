//go:build windows

package console

import (
	"os"
	"syscall"
)

// The Windows console, asked directly, for the reason terminal_unix.go gives:
// no third-party dependency for two calls into kernel32.
//
// GetConsoleMode is in the standard library. SetConsoleMode is not, so it is
// looked up lazily -- which costs nothing on a machine that never prompts.
var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

// enableEchoInput is the console mode bit that shows what is typed.
const enableEchoInput = 0x0004

// isTerminal reports whether f is a console.
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(f.Fd()), &mode) == nil
}

// hideEcho clears the echo bit, and returns the function that puts the mode
// back exactly as it was.
func hideEcho(f *os.File) (func(), bool) {
	if f == nil {
		return nil, false
	}

	handle := syscall.Handle(f.Fd())
	var before uint32
	if err := syscall.GetConsoleMode(handle, &before); err != nil {
		return nil, false
	}

	if r, _, _ := procSetConsoleMode.Call(uintptr(handle), uintptr(before&^enableEchoInput)); r == 0 {
		return nil, false
	}

	return func() {
		procSetConsoleMode.Call(uintptr(handle), uintptr(before))
	}, true
}
