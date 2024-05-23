// Borrowed from https://github.com/creack/termios/blob/master/win/win.go

//go:build !windows

package sshd

import (
	"log"
	"syscall"
	"unsafe"
)

// WinSize stores the Height and Width of a terminal.
type WinSize struct {
	Height uint16
	Width  uint16
	x      uint16 // unused
	y      uint16 // unused
}

// SetWinSize sets the size of the given pty.
func SetWinSize(fd uintptr, width, height uint32) (errno error) {
	log.Printf("window resize %dx%d", width, height)

	ws := WinSize{
		Width:  uint16(width),
		Height: uint16(height),
	}

	_, _, errno = syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&ws)),
	)

	return
}
