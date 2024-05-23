package sshd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)

const DefaultShell string = "sh"

// Server is the sshd server.
type Server struct {
	shellPath string
	config    *ssh.ServerConfig
	logger    *log.Logger
	listener  net.Listener

	mu     sync.Mutex
	closed bool
}

type Config any

// NewServer creates a sshd server.
// The shellPath is the path of the shell (e.g., "bash").
// You can pass nil as logger if you want to disable log outputs.
func NewServer(
	shellPath string,
	config Config,
	logger *log.Logger,
) (srv *Server) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	conf, ok := config.(*ssh.ServerConfig)
	if !ok {
		panic("invalid config: type assertion failed")
	}

	srv = &Server{shellPath: shellPath, config: conf, logger: logger}
	return
}

// ListenAndServe let the server listen and serve.
func (srv *Server) ListenAndServe(addr string) (err error) {
	if addr == "" {
		err = errors.New("empty addr")
		return
	}

	// Once a ServerConfig has been configured, connections can be accepted.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		err = fmt.Errorf("listen on %s: %s", addr, err)
		return
	}

	err = srv.Serve(listener)
	return
}

func (srv *Server) Serve(listener net.Listener) (err error) {
	if listener == nil {
		err = errors.New("listen on nil listener")
		return
	}

	srv.listener = listener

	var conn net.Conn

	for {
		conn, err = listener.Accept()
		if err != nil {
			if srv.isClosed() {
				return nil
			}
			err = fmt.Errorf("accept incoming connection: %s", err)
			return
		}
		// Before use, a handshake must be performed on the incoming net.Conn.
		go func() { _ = srv.handleConn(conn) }()
	}
}

func (srv *Server) handleConn(conn net.Conn) (err error) {
	_, channels, requests, err := ssh.NewServerConn(conn, srv.config)
	if err != nil {
		err = fmt.Errorf("failed to handshake: %s", err)
		return
	}

	go ssh.DiscardRequests(requests)

	go srv.handleChannels(channels)

	return
}

func (srv *Server) isClosed() bool {
	srv.mu.Lock()
	closed := srv.closed
	defer srv.mu.Unlock()
	return closed
}

func (srv *Server) handleChannels(in <-chan ssh.NewChannel) {
	// Service the incoming ssh.NewChannel channel.
	for newChan := range in {
		// Channels have a type, depending on the application level
		// protocol intended. In the case of a shell, the type is
		// "session" and ServerShell may be used to present a simple
		// terminal interface.
		if t := newChan.ChannelType(); t != "session" {
			_ = newChan.Reject(
				ssh.UnknownChannelType,
				fmt.Sprintf("unknown channel type: %s", t),
			)
			continue
		}
		channel, requests, err := newChan.Accept()
		if err != nil {
			log.Printf("could not accept channel (%s)", err)
			continue
		}

		// allocate a terminal for this channel
		log.Print("creating pty...")
		// Create new pty
		f, tty, err := pty.Open()
		if err != nil {
			log.Printf("could not start pty (%s)", err)
			continue
		}

		var shell string
		shell = os.Getenv("SHELL")
		if shell == "" {
			shell = DefaultShell
		}

		// Sessions have out-of-band requests such as "shell", "pty-req" and "env"
		go func(in <-chan *ssh.Request) {
			for req := range in {
				log.Printf("%v %s", req.Payload, req.Payload)
				ok := false
				switch req.Type {
				case "exec":
					ok = true
					command := string(req.Payload[4 : req.Payload[3]+4])
					cmd := exec.Command(shell, []string{"-c", command}...)

					cmd.Stdout = channel
					cmd.Stderr = channel
					cmd.Stdin = channel

					err := cmd.Start()
					if err != nil {
						log.Printf("could not start command (%s)", err)
						continue
					}

					// teardown session
					go func() {
						_, err := cmd.Process.Wait()
						if err != nil {
							log.Printf("failed to exit bash (%s)", err)
						}
						_ = channel.Close()
						log.Printf("session closed")
					}()
				case "shell":
					cmd := exec.Command(shell)
					cmd.Env = []string{"TERM=xterm"}
					err := PtyRun(cmd, tty)
					if err != nil {
						log.Printf("%s", err)
					}

					// Teardown session
					var once sync.Once
					chanClose := func() {
						_ = channel.Close()
						log.Printf("session closed")
					}

					// Pipe session to bash and visa-versa
					go func() {
						_, _ = io.Copy(channel, f)
						once.Do(chanClose)
					}()

					go func() {
						_, _ = io.Copy(f, channel)
						once.Do(chanClose)
					}()

					// We don't accept any commands (Payload),
					// only the default shell.
					if len(req.Payload) == 0 {
						ok = true
					}
				case "pty-req":
					// Responding 'ok' here will let the client
					// know we have a pty ready for input
					ok = true
					// Parse body...
					termLen := req.Payload[3]
					termEnv := string(req.Payload[4 : termLen+4])
					w, h := parseDims(req.Payload[termLen+4:])
					_ = SetWinSize(f.Fd(), w, h)
					log.Printf("pty-req '%s'", termEnv)
				case "window-change":
					w, h := parseDims(req.Payload)
					_ = SetWinSize(f.Fd(), w, h)
					continue //no response
				}

				if !ok {
					log.Printf("declining %s request...", req.Type)
				}

				_ = req.Reply(ok, nil)
			}
		}(requests)
	}
}

// Close stops the server.
func (srv *Server) Close() error {
	srv.mu.Lock()
	srv.closed = true
	defer srv.mu.Unlock()
	if srv.listener == nil {
		return nil
	}
	return srv.listener.Close()
}

// parseDims extracts terminal dimensions (width x height) from the provided buffer.
func parseDims(b []byte) (uint32, uint32) {
	w := binary.BigEndian.Uint32(b)
	h := binary.BigEndian.Uint32(b[4:])
	return w, h
}

// PtyRun start assigns a pseudo-terminal tty os.File to c.Stdin, c.Stdout,
// and c.Stderr, calls c.Start, and returns the File of the tty
// corresponding pty.
func PtyRun(c *exec.Cmd, tty *os.File) (err error) {
	// Make sure to close the pty at the end.
	defer func() { _ = tty.Close() }() // Best effort.
	c.Stdout = tty
	c.Stdin = tty
	c.Stderr = tty
	c.SysProcAttr = &syscall.SysProcAttr{
		Setctty: true,
		Setsid:  true,
	}
	return c.Start()
}
