package protection

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	stateDirPerm  os.FileMode = 0700
	stateFilePerm os.FileMode = 0600
)

type PersistedState struct {
	ProtectionEnabled bool `json:"protection_enabled"`
}

type StateStore struct {
	path string
}

func NewStateStore(path string) *StateStore {
	return &StateStore{path: path}
}

func (s *StateStore) Load() (PersistedState, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PersistedState{}, nil
		}
		return PersistedState{}, err
	}

	var state PersistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return PersistedState{}, fmt.Errorf("decode protection state: %w", err)
	}
	return state, nil
}

func (s *StateStore) Save(state PersistedState) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		return err
	}
	if err := os.Chmod(dir, stateDirPerm); err != nil {
		return err
	}

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(dir, ".protection-*.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Chmod(stateFilePerm); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return err
	}
	removeTemp = false
	return os.Chmod(s.path, stateFilePerm)
}
