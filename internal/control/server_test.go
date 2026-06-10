package control

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListenUnixSocketRemovesStaleSocket(t *testing.T) {
	dir := shortTempDir(t)
	socketPath := filepath.Join(dir, "webot-msg.sock")

	staleListener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("create stale socket: %v", err)
	}
	if err := staleListener.Close(); err != nil {
		t.Fatalf("close stale socket listener: %v", err)
	}

	listener, err := listenUnixSocket(socketPath)
	if err != nil {
		t.Fatalf("listenUnixSocket() error = %v", err)
	}
	defer listener.Close()

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("socket perm = %o, want 600", got)
	}
}

func TestListenUnixSocketRejectsRegularFile(t *testing.T) {
	dir := shortTempDir(t)
	socketPath := filepath.Join(dir, "webot-msg.sock")
	if err := os.WriteFile(socketPath, []byte("not a socket"), 0600); err != nil {
		t.Fatalf("write regular file: %v", err)
	}

	listener, err := listenUnixSocket(socketPath)
	if err == nil {
		listener.Close()
		t.Fatal("listenUnixSocket() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "not a unix socket") {
		t.Fatalf("listenUnixSocket() error = %q, want not a unix socket", err.Error())
	}

	data, err := os.ReadFile(socketPath)
	if err != nil {
		t.Fatalf("regular file was removed: %v", err)
	}
	if string(data) != "not a socket" {
		t.Fatalf("regular file data = %q, want preserved", string(data))
	}
}

func shortTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "wm-")
	if err != nil {
		t.Fatalf("create short temp dir: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(dir)
	})
	return dir
}
