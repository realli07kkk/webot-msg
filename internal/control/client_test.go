package control

import (
	"bytes"
	"io"
	"net"
	"path/filepath"
	"testing"
)

func TestAttachDoesNotWriteControlHeader(t *testing.T) {
	socketPath := filepath.Join(shortTempDir(t), "webot-msg.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	defer listener.Close()

	received := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			received <- "accept error: " + err.Error()
			return
		}
		defer conn.Close()
		data, err := io.ReadAll(conn)
		if err != nil {
			received <- "read error: " + err.Error()
			return
		}
		received <- string(data)
	}()

	err = Attach(socketPath, bytes.NewBufferString("/exit\n"), io.Discard)
	if err != nil {
		t.Fatalf("Attach() error = %v", err)
	}

	if got := <-received; got != "/exit\n" {
		t.Fatalf("attached client wrote %q, want only user input", got)
	}
}
