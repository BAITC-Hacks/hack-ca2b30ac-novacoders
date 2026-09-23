package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalDataDirectoryFromRepoAndBackend(t *testing.T) {
	repo := t.TempDir()
	backend := filepath.Join(repo, "backend")
	if err := os.Mkdir(backend, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backend, "go.mod"), []byte("module example.test/backend\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Resolution must not depend on whether the workbooks have been copied yet.
	for _, cwd := range []string{repo, backend} {
		if got, want := localDataDirectory(cwd, ""), filepath.Join(backend, "data", "demo"); got != want {
			t.Fatalf("cwd=%s: got %s, want %s", cwd, got, want)
		}
		if got, want := localDataDirectory(cwd, "custom"), filepath.Join(cwd, "custom"); got != want {
			t.Fatalf("explicit relative path changed: got %s, want %s", got, want)
		}
	}
	explicit := t.TempDir()
	if got := localDataDirectory(repo, explicit); got != explicit {
		t.Fatalf("explicit absolute path changed: %s", got)
	}
}
