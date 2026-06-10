package logfile

import (
	"os"
	"path/filepath"
	"sync"
)

type SizeWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	file     *os.File
	size     int64
}

func NewSizeWriter(path string, maxBytes int64) (*SizeWriter, error) {
	if path == "" {
		return nil, nil
	}
	if maxBytes <= 0 {
		return nil, os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	return &SizeWriter{
		path:     path,
		maxBytes: maxBytes,
		file:     file,
		size:     info.Size(),
	}, nil
}

func (w *SizeWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *SizeWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *SizeWriter) rotate() error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}

	backupPath := w.path + ".1"
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(w.path); err == nil {
		if err := os.Rename(w.path, backupPath); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	w.file = file
	w.size = 0
	return nil
}
