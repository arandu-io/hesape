//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package console

import (
	"os"
	"syscall"
	"unsafe"
)

// The terminal, asked directly rather than through a library.
//
// Reading the terminal state is two ioctls, and the ioctl numbers are the
// only part that differs between the systems that have a termios at all --
// too little to justify a dependency.

// isTerminal reports whether f is a terminal.
//
// Reading the termios of something that is not one fails, which is the same
// test every implementation of this makes.
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(ioctlReadTermios), uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

// hideEcho stops the terminal from showing what is typed, and returns the
// function that puts it back.
//
// The caller defers the restore, and it has to: a process that exits with echo
// off leaves the shell it was started from typing invisibly.
func hideEcho(f *os.File) (func(), bool) {
	if f == nil {
		return nil, false
	}

	var before syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(ioctlReadTermios), uintptr(unsafe.Pointer(&before))); errno != 0 {
		return nil, false
	}

	after := before
	after.Lflag &^= syscall.ECHO
	// Canonical mode and signals stay on: the line is still edited and still
	// interrupted by ctrl-c, which is the only way out of a prompt.
	after.Lflag |= syscall.ICANON | syscall.ISIG

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(ioctlWriteTermios), uintptr(unsafe.Pointer(&after))); errno != 0 {
		return nil, false
	}

	return func() {
		restored := before
		syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(ioctlWriteTermios), uintptr(unsafe.Pointer(&restored)))
	}, true
}
