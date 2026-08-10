//go:build linux

package console

import "syscall"

// The Linux spelling of the two ioctls that read and write a termios.
const (
	ioctlReadTermios  = syscall.TCGETS
	ioctlWriteTermios = syscall.TCSETS
)
