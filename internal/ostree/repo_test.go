package ostree

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestInit(t *testing.T) {

	tmp, err := ioutil.TempDir("", t.Name())
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmp)

	path := filepath.Join(tmp, "repo")

	repo := NewRepo(path)

	err = repo.Init(ARCHIVE)

	if err != nil {
		t.Errorf("repo init failed: %w", err)
	}

	err = repo.Init(ARCHIVE)

	if err != nil {
		t.Errorf("repo init failed: %w", err)
	}
}
