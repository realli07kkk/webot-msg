package console

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrInterrupted = errors.New("console interrupted")

type LineReader interface {
	ReadLine(prompt string) (string, error)
	Close() error
}

type BufferedLineReader struct {
	reader *bufio.Reader
	out    io.Writer
}

func NewBufferedLineReader(in io.Reader, out io.Writer) *BufferedLineReader {
	return &BufferedLineReader{
		reader: bufio.NewReader(in),
		out:    out,
	}
}

func (r *BufferedLineReader) ReadLine(prompt string) (string, error) {
	if prompt != "" {
		fmt.Fprint(r.out, prompt)
	}

	line, err := r.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (r *BufferedLineReader) Close() error {
	return nil
}
