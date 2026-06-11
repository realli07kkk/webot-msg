package control

import (
	"io"
	"net"
	"path/filepath"
	"sync"
	"testing"
)

func TestRunInteractiveSessionWritesOnlySubmittedLines(t *testing.T) {
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
		if _, err := io.WriteString(conn, "[No Bot Selected] > "); err != nil {
			received <- "write error: " + err.Error()
			return
		}
		data, err := io.ReadAll(conn)
		if err != nil {
			received <- "read error: " + err.Error()
			return
		}
		received <- string(data)
	}()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial unix socket: %v", err)
	}
	reader := &scriptedInteractiveReader{
		results: []lineReadResult{
			{line: "/protection status"},
			{err: io.EOF},
		},
	}

	if err := runInteractiveSession(conn, reader); err != nil {
		t.Fatalf("runInteractiveSession() error = %v", err)
	}
	if got := <-received; got != "/protection status\n" {
		t.Fatalf("interactive client wrote %q, want only submitted line", got)
	}
	if !reader.closed {
		t.Fatal("interactive reader was not closed")
	}
}

type scriptedInteractiveReader struct {
	mu      sync.Mutex
	results []lineReadResult
	writes  []string
	prompts []string
	closed  bool
}

func (r *scriptedInteractiveReader) ReadLine(string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.results) == 0 {
		return "", io.EOF
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result.line, result.err
}

func (r *scriptedInteractiveReader) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, string(p))
	return len(p), nil
}

func (r *scriptedInteractiveReader) SetPrompt(prompt string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts = append(r.prompts, prompt)
}

func (r *scriptedInteractiveReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}
