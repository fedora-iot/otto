package main

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestMissing(t *testing.T) {
	cfg := OttoConfig{}

	err := cfg.LoadConfig("/nonexistent.toml")
	if err != nil {
		t.Fatalf("Loading missing config should be ok")
	}
}

func TestPartial(t *testing.T) {

	tmp, err := ioutil.TempDir("", t.Name())
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmp)

	path := filepath.Join(tmp, "partial.toml")

	fd, err := os.Create(path)
	if err != nil {
		log.Fatalf("Failed to create file: %v", err)
	}

	cfg := OttoConfig{
		Root: "/tmp/otto",
	}

	err = cfg.DumpConfig(fd)
	if err != nil {
		log.Fatalf("Failed to dump config: %v", err)
	}

	fd.Close()

	new_cfg := OttoConfig{
		Root: "/old/otto",
		Addr: ":4000",
	}

	err = new_cfg.LoadConfig(path)
	if err != nil {
		t.Fatalf("Failed to load config")
	}

	if new_cfg.Root != "/tmp/otto" {
		log.Fatalf("Root should have been updated, is: %s", new_cfg.Root)
	}

	if new_cfg.Addr != ":4000" {
		log.Fatalf("Addr should have not be touched, is: %s", new_cfg.Addr)
	}
}
