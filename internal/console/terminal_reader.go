package console

import (
	"errors"
	"io"
	"os"
	"sync"

	"golang.org/x/term"
)

type TerminalLineReader struct {
	terminal *term.Terminal
	reader   *interruptReader
	restore  func() error
	onClose  func()
	once     sync.Once
}

func NewTerminalLineReader(rw io.ReadWriter, restore func() error) *TerminalLineReader {
	reader := &interruptReader{reader: rw}
	t := term.NewTerminal(readWriter{reader: reader, writer: rw}, "")
	t.AutoCompleteCallback = func(line string, pos int, key rune) (string, int, bool) {
		if key != '\t' {
			return line, pos, false
		}
		return CompleteCommandLine(line, pos)
	}
	return &TerminalLineReader{
		terminal: t,
		reader:   reader,
		restore:  restore,
	}
}

func NewLocalTerminalLineReader(in *os.File, out *os.File) (*TerminalLineReader, bool) {
	if !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return nil, false
	}

	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return nil, false
	}

	reader := NewTerminalLineReader(stdioReadWriter{in: in, out: out}, func() error {
		return term.Restore(int(in.Fd()), state)
	})
	reader.onClose = registerActiveTerminal(reader)
	if width, height, err := term.GetSize(int(out.Fd())); err == nil {
		_ = reader.terminal.SetSize(width, height)
	}
	return reader, true
}

func (r *TerminalLineReader) ReadLine(prompt string) (string, error) {
	r.reader.Reset()
	r.terminal.SetPrompt(prompt)
	line, err := r.terminal.ReadLine()
	if errors.Is(err, io.EOF) && r.reader.Interrupted() {
		return "", ErrInterrupted
	}
	return line, err
}

func (r *TerminalLineReader) Write(p []byte) (int, error) {
	return r.terminal.Write(p)
}

func (r *TerminalLineReader) Close() error {
	var err error
	r.once.Do(func() {
		if r.restore != nil {
			err = r.restore()
		}
		if r.onClose != nil {
			r.onClose()
		}
	})
	return err
}

var activeTerminal struct {
	sync.Mutex
	reader *TerminalLineReader
}

func registerActiveTerminal(reader *TerminalLineReader) func() {
	activeTerminal.Lock()
	activeTerminal.reader = reader
	activeTerminal.Unlock()

	return func() {
		activeTerminal.Lock()
		if activeTerminal.reader == reader {
			activeTerminal.reader = nil
		}
		activeTerminal.Unlock()
	}
}

func RestoreActiveTerminal() error {
	activeTerminal.Lock()
	reader := activeTerminal.reader
	activeTerminal.Unlock()

	if reader == nil {
		return nil
	}
	return reader.Close()
}

type stdioReadWriter struct {
	in  io.Reader
	out io.Writer
}

func (rw stdioReadWriter) Read(p []byte) (int, error) {
	return rw.in.Read(p)
}

func (rw stdioReadWriter) Write(p []byte) (int, error) {
	return rw.out.Write(p)
}

type readWriter struct {
	reader io.Reader
	writer io.Writer
}

func (rw readWriter) Read(p []byte) (int, error) {
	return rw.reader.Read(p)
}

func (rw readWriter) Write(p []byte) (int, error) {
	return rw.writer.Write(p)
}

type interruptReader struct {
	reader      io.Reader
	interrupted bool
}

func (r *interruptReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	for _, b := range p[:n] {
		if b == 3 {
			r.interrupted = true
			break
		}
	}
	return n, err
}

func (r *interruptReader) Reset() {
	r.interrupted = false
}

func (r *interruptReader) Interrupted() bool {
	return r.interrupted
}
