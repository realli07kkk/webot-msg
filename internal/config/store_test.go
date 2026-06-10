package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreCreatesAuthFileWithOwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "auth.json")
	store := NewStore(path)

	if err := store.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}
	if err := store.AddBot(UserConfig{BotID: "bot-1"}); err != nil {
		t.Fatalf("AddBot() error = %v", err)
	}

	assertPerm(t, filepath.Dir(path), 0700)
	assertPerm(t, path, 0600)
}

func TestStoreSaveTightensExistingAuthFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "auth.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"bots":{}}`), 0644); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	store := NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	assertPerm(t, path, 0600)
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s perm = %o, want %o", path, got, want)
	}
}
