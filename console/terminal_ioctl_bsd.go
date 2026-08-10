//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package console

import "syscall"

// The BSD spelling of the two ioctls that read and write a termios. Darwin is
// a BSD here, and this is the only line that differs from Linux.
const (
	ioctlReadTermios  = syscall.TIOCGETA
	ioctlWriteTermios = syscall.TIOCSETA
)
