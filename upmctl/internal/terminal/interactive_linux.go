//go:build linux

package terminal

import (
	"syscall"
	"unsafe"
)

func isInteractive(fd uintptr) bool {
	var state syscall.Termios
	_, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL,
		fd,
		uintptr(syscall.TCGETS),
		uintptr(unsafe.Pointer(&state)),
		0,
		0,
		0,
	)
	return errno == 0
}
