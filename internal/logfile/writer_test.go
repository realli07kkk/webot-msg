package logfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSizeWriterRotatesWhenMaxSizeWouldBeExceeded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webot-msg.log")

	writer, err := NewSizeWriter(path, 10)
	if err != nil {
		t.Fatalf("NewSizeWriter() error = %v", err)
	}

	if _, err := writer.Write([]byte("12345")); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	if _, err := writer.Write([]byte("678901")); err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read current log: %v", err)
	}
	if string(current) != "678901" {
		t.Fatalf("current log = %q, want second write", string(current))
	}

	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("read backup log: %v", err)
	}
	if string(backup) != "12345" {
		t.Fatalf("backup log = %q, want first write", string(backup))
	}
}

func TestNewSizeWriterEmptyPathDisablesFileLogging(t *testing.T) {
	writer, err := NewSizeWriter("", 10)
	if err != nil {
		t.Fatalf("NewSizeWriter() error = %v", err)
	}
	if writer != nil {
		t.Fatal("NewSizeWriter() writer != nil")
	}
}
