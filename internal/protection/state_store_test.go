package protection

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateStoreLoadMissingFileReturnsDisabled(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state", "protection.json"))

	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if state.ProtectionEnabled {
		t.Fatal("ProtectionEnabled = true, want false")
	}
}

func TestStateStoreLoadDamagedJSONReturnsDisabledAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "protection.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{bad json"), 0600); err != nil {
		t.Fatalf("write damaged state: %v", err)
	}

	state, err := NewStateStore(path).Load()
	if err == nil {
		t.Fatal("Load() error = nil, want damaged JSON error")
	}
	if state.ProtectionEnabled {
		t.Fatal("ProtectionEnabled = true, want zero value on damaged JSON")
	}
}

func TestStateStoreSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "protection.json")
	store := NewStateStore(path)

	if err := store.Save(PersistedState{ProtectionEnabled: true}); err != nil {
		t.Fatalf("Save(true) error = %v", err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after Save(true) error = %v", err)
	}
	if !state.ProtectionEnabled {
		t.Fatal("ProtectionEnabled = false, want true")
	}

	if err := store.Save(PersistedState{ProtectionEnabled: false}); err != nil {
		t.Fatalf("Save(false) error = %v", err)
	}
	state, err = store.Load()
	if err != nil {
		t.Fatalf("Load() after Save(false) error = %v", err)
	}
	if state.ProtectionEnabled {
		t.Fatal("ProtectionEnabled = true, want false")
	}
}

func TestStateStoreSaveSetsPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "protection.json")
	if err := NewStateStore(path).Save(PersistedState{ProtectionEnabled: true}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	assertFilePerm(t, filepath.Dir(path), 0700)
	assertFilePerm(t, path, 0600)
}

func TestStateStoreSaveOverwritesDamagedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "protection.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{bad json"), 0600); err != nil {
		t.Fatalf("write damaged state: %v", err)
	}

	store := NewStateStore(path)
	if err := store.Save(PersistedState{ProtectionEnabled: true}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after overwrite error = %v", err)
	}
	if !state.ProtectionEnabled {
		t.Fatal("ProtectionEnabled = false, want true")
	}
}

func assertFilePerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s perm = %o, want %o", path, got, want)
	}
}
