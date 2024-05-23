//go:build windows

package sshd

import (
	"fmt"
)

type WinSize struct {
	Width  uint16
	Height uint16
}

// TODO: test this function
func SetWinSize(width, height int32) (err error) {
	ws := WinSize{Width: uint16(width), Height: uint16(height)}

	var consoleHandle syscall.Handle

	err = syscall.GetStdHandle(syscall.STD_INPUT_HANDLE, &consoleHandle)
	if err != nil {
		err = fmt.Errorf("GetStdHandle failed: %v", err)
		return
	}

	success, err := syscall.SetConsoleWindowInfo(consoleHandle, true, &ws)
	if err != nil {
		err = fmt.Errorf("SetConsoleWindowInfo failed: %v", err)
		return
	}

	if !success {
		err = errors.New("SetConsoleWindowInfo failed")
	}

	return
}
