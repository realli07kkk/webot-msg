package control

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/realli07kkk/webot-msg/internal/console"
)

type Server struct {
	socketPath string
	controller console.Controller
	listener   net.Listener
}

var ErrSocketAlreadyInUse = errors.New("control socket already in use")

func NewServer(socketPath string, controller console.Controller) *Server {
	return &Server{
		socketPath: socketPath,
		controller: controller,
	}
}

func (s *Server) Start() error {
	listener, err := listenUnixSocket(s.socketPath)
	if err != nil {
		return err
	}
	s.listener = listener

	go s.acceptLoop()
	return nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("Control console accept failed: %v", err)
			continue
		}

		go func() {
			defer conn.Close()
			out := newSynchronizedWriter(conn)
			unregister := registerConsoleOutput(s.controller, out)
			defer unregister()
			console.RunWithIO(s.controller, conn, out)
		}()
	}
}

type consoleOutputController interface {
	AddConsoleOutput(io.Writer) func()
}

func registerConsoleOutput(controller console.Controller, out io.Writer) func() {
	if controller, ok := controller.(consoleOutputController); ok {
		return controller.AddConsoleOutput(out)
	}
	return func() {}
}

type synchronizedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func newSynchronizedWriter(w io.Writer) *synchronizedWriter {
	return &synchronizedWriter{w: w}
}

func (w *synchronizedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

func listenUnixSocket(socketPath string) (net.Listener, error) {
	if socketPath == "" {
		return nil, fmt.Errorf("control socket path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		return nil, fmt.Errorf("create control socket directory: %w", err)
	}

	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("control socket path exists and is not a unix socket: %s", socketPath)
		}
		if isUnixSocketAlive(socketPath) {
			return nil, fmt.Errorf("%w: %s", ErrSocketAlreadyInUse, socketPath)
		}
		if err := os.Remove(socketPath); err != nil {
			return nil, fmt.Errorf("remove stale control socket: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat control socket: %w", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen control socket: %w", err)
	}
	if err := os.Chmod(socketPath, 0600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("chmod control socket: %w", err)
	}
	return listener, nil
}

func isUnixSocketAlive(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
