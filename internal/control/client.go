package control

import (
	"fmt"
	"io"
	"net"
)

func Attach(socketPath string, in io.Reader, out io.Writer) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect control socket %s: %w", socketPath, err)
	}
	defer conn.Close()

	outDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(out, conn)
		outDone <- err
	}()

	go func() {
		_, _ = io.Copy(conn, in)
		if unixConn, ok := conn.(*net.UnixConn); ok {
			_ = unixConn.CloseWrite()
		}
	}()

	if err := <-outDone; err != nil {
		return fmt.Errorf("control console closed: %w", err)
	}
	return nil
}
