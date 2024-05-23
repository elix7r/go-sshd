//go:build ignore

package sshd

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)

type ShellFile struct {
	file *os.File
}

// start shell
func (srv *Server) startShell(connection ssh.Channel) *ShellFile {
	shell := exec.Command(srv.shellPath)

	// Prepare teardown function
	connClose := func() {
		_ = connection.Close()
		_, err := shell.Process.Wait()
		if err != nil {
			srv.logger.Printf("Failed to exit shell (%s)", err)
		}
		srv.logger.Printf("Session closed")
	}

	// Allocate a terminal for this channel
	srv.logger.Print("Creating pty...")
	file, err := pty.Start(shell)
	if err != nil {
		srv.logger.Printf("Could not start pty (%s)", err)
		connClose()
		return nil
	}

	//pipe session to shell and visa-versa
	var once sync.Once
	go func() {
		_, _ = io.Copy(connection, file)
		once.Do(connClose)
	}()
	go func() {
		_, _ = io.Copy(file, connection)
		once.Do(connClose)
	}()
	return &ShellFile{file}
}

// SetWinSize sets the size of the given pty.
func (sf *ShellFile) SetWinSize(w, h uint32) {
	fd := sf.file.Fd()
	ws := WinSize{Width: uint16(w), Height: uint16(h)}
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(&ws)))
}
