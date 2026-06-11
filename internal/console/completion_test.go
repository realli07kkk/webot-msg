package console

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestCompleteCommandLineCompletesTopLevelCommands(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    string
		wantPos int
	}{
		{name: "login", line: "/log", want: "/login", wantPos: len("/login")},
		{name: "exit", line: "/ex", want: "/exit", wantPos: len("/exit")},
		{name: "protection", line: "/pro", want: "/protection ", wantPos: len("/protection ")},
		{name: "shared prefix", line: "/b", want: "/bot", wantPos: len("/bot")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotPos, ok := CompleteCommandLine(tt.line, len(tt.line))
			if !ok {
				t.Fatal("CompleteCommandLine() ok = false, want true")
			}
			if got != tt.want || gotPos != tt.wantPos {
				t.Fatalf("CompleteCommandLine() = (%q, %d), want (%q, %d)", got, gotPos, tt.want, tt.wantPos)
			}
		})
	}
}

func TestCompleteCommandLineCompletesProtectionSubcommands(t *testing.T) {
	got, gotPos, ok := CompleteCommandLine("/protection st", len("/protection st"))
	if !ok {
		t.Fatal("CompleteCommandLine() ok = false, want true")
	}
	if got != "/protection status" || gotPos != len("/protection status") {
		t.Fatalf("CompleteCommandLine() = (%q, %d), want (%q, %d)", got, gotPos, "/protection status", len("/protection status"))
	}
}

func TestCompleteCommandLineDoesNotChooseEmptySubcommandPrefix(t *testing.T) {
	got, gotPos, ok := CompleteCommandLine("/protection ", len("/protection "))
	if !ok {
		t.Fatal("CompleteCommandLine() ok = false, want true")
	}
	if got != "/protection " || gotPos != len("/protection ") {
		t.Fatalf("CompleteCommandLine() = (%q, %d), want unchanged", got, gotPos)
	}
}

func TestCompleteCommandLineLeavesUnknownCommandUnchanged(t *testing.T) {
	got, gotPos, ok := CompleteCommandLine("/unknown", len("/unknown"))
	if !ok {
		t.Fatal("CompleteCommandLine() ok = false, want true")
	}
	if got != "/unknown" || gotPos != len("/unknown") {
		t.Fatalf("CompleteCommandLine() = (%q, %d), want unchanged", got, gotPos)
	}
}

func TestCompleteCommandLineLeavesTextUnchanged(t *testing.T) {
	got, gotPos, ok := CompleteCommandLine("hello", len("hello"))
	if !ok {
		t.Fatal("CompleteCommandLine() ok = false, want true")
	}
	if got != "hello" || gotPos != len("hello") {
		t.Fatalf("CompleteCommandLine() = (%q, %d), want unchanged", got, gotPos)
	}
}

func TestCompleteCommandLinePreservesSuffix(t *testing.T) {
	got, gotPos, ok := CompleteCommandLine("/pro tail", len("/pro"))
	if !ok {
		t.Fatal("CompleteCommandLine() ok = false, want true")
	}
	if got != "/protection tail" || gotPos != len("/protection") {
		t.Fatalf("CompleteCommandLine() = (%q, %d), want suffix preserved", got, gotPos)
	}
}

func TestTerminalLineReaderCompletesTabBeforeEnter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "login", input: "/log\t\r", want: "/login"},
		{name: "protection", input: "/pro\t\r", want: "/protection "},
		{name: "protection status", input: "/protection st\t\r", want: "/protection status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rw := &memoryReadWriter{reader: bytes.NewBufferString(tt.input)}
			lineReader := NewTerminalLineReader(rw, nil)

			got, err := lineReader.ReadLine("> ")
			if err != nil {
				t.Fatalf("ReadLine() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ReadLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTerminalLineReaderReturnsInterruptedForCtrlC(t *testing.T) {
	rw := &memoryReadWriter{reader: bytes.NewBufferString("\x03")}
	lineReader := NewTerminalLineReader(rw, nil)

	_, err := lineReader.ReadLine("> ")
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("ReadLine() error = %v, want ErrInterrupted", err)
	}
}

func TestRestoreActiveTerminalClosesRegisteredReader(t *testing.T) {
	restoreCalls := 0
	rw := &memoryReadWriter{reader: bytes.NewBuffer(nil)}
	lineReader := NewTerminalLineReader(rw, func() error {
		restoreCalls++
		return nil
	})
	lineReader.onClose = registerActiveTerminal(lineReader)

	if err := RestoreActiveTerminal(); err != nil {
		t.Fatalf("RestoreActiveTerminal() error = %v", err)
	}
	if restoreCalls != 1 {
		t.Fatalf("restore calls = %d, want 1", restoreCalls)
	}

	if err := RestoreActiveTerminal(); err != nil {
		t.Fatalf("RestoreActiveTerminal() second call error = %v", err)
	}
	if restoreCalls != 1 {
		t.Fatalf("restore calls after second call = %d, want 1", restoreCalls)
	}
}

type memoryReadWriter struct {
	reader io.Reader
	writer bytes.Buffer
}

func (rw *memoryReadWriter) Read(p []byte) (int, error) {
	return rw.reader.Read(p)
}

func (rw *memoryReadWriter) Write(p []byte) (int, error) {
	return rw.writer.Write(p)
}
