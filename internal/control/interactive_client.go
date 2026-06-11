package control

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"github.com/realli07kkk/webot-msg/internal/console"
)

type interactiveLineReader interface {
	ReadLine(prompt string) (string, error)
	Write([]byte) (int, error)
	SetPrompt(prompt string)
	Close() error
}

type lineReadResult struct {
	line string
	err  error
}

func AttachInteractive(socketPath string, in *os.File, out *os.File) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect control socket %s: %w", socketPath, err)
	}

	reader, ok := console.NewLocalTerminalLineReader(in, out)
	if !ok {
		conn.Close()
		return fmt.Errorf("prepare interactive terminal")
	}

	return runInteractiveSession(conn, reader)
}

func runInteractiveSession(conn net.Conn, reader interactiveLineReader) error {
	defer conn.Close()
	defer reader.Close()

	var promptMu sync.Mutex
	prompt := ""
	currentPrompt := func() string {
		promptMu.Lock()
		defer promptMu.Unlock()
		return prompt
	}
	updatePrompt := func(next string) {
		promptMu.Lock()
		prompt = next
		promptMu.Unlock()
		reader.SetPrompt(next)
	}

	firstPrompt := make(chan struct{})
	var firstPromptOnce sync.Once
	markFirstPrompt := func(next string) {
		updatePrompt(next)
		firstPromptOnce.Do(func() {
			close(firstPrompt)
		})
	}

	var splitter outputSplitter
	var splitterMu sync.Mutex

	readDone := make(chan error, 1)
	handleOutputChunk := func(p []byte) error {
		splitterMu.Lock()
		events := splitter.Push(p)
		splitterMu.Unlock()
		for _, line := range events.lines {
			if _, err := reader.Write([]byte(line + "\n")); err != nil {
				return fmt.Errorf("write control console output: %w", err)
			}
		}
		if events.promptChanged {
			markFirstPrompt(events.prompt)
		}
		return nil
	}
	go func() {
		readDone <- readInteractiveOutput(conn, handleOutputChunk)
	}()

	select {
	case <-firstPrompt:
	case err := <-readDone:
		return err
	}

	for {
		lineDone := make(chan lineReadResult, 1)
		go func(prompt string) {
			line, err := reader.ReadLine(prompt)
			lineDone <- lineReadResult{line: line, err: err}
		}(currentPrompt())

		select {
		case result := <-lineDone:
			if result.err != nil {
				if errors.Is(result.err, console.ErrInterrupted) {
					conn.Close()
					return nil
				}
				if err := closeWrite(conn); err != nil {
					return fmt.Errorf("close control socket write side: %w", err)
				}
				return <-readDone
			}
			splitterMu.Lock()
			splitter.ResetTail()
			splitterMu.Unlock()
			if _, err := io.WriteString(conn, result.line+"\n"); err != nil {
				return fmt.Errorf("write control console line: %w", err)
			}
		case err := <-readDone:
			return err
		}
	}
}

func readInteractiveOutput(conn net.Conn, handleChunk func([]byte) error) error {
	buf := make([]byte, 4096)

	for {
		n, readErr := conn.Read(buf)
		if n > 0 {
			if err := handleChunk(buf[:n]); err != nil {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("read control console output: %w", readErr)
		}
	}
}

func closeWrite(conn net.Conn) error {
	if unixConn, ok := conn.(*net.UnixConn); ok {
		return unixConn.CloseWrite()
	}
	return conn.Close()
}
