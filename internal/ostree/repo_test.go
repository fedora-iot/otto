package ostree

import (
	"path/filepath"
	"testing"
)

func TestInit(t *testing.T) {

	tmp := t.TempDir()
	path := filepath.Join(tmp, "repo")

	repo := NewRepo(path)

	err := repo.Init(ARCHIVE)

	if err != nil {
		t.Errorf("repo init failed: %w", err)
	}

	err = repo.Init(ARCHIVE)

	if err != nil {
		t.Errorf("repo init failed: %w", err)
	}
}
