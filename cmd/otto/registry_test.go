package main

import (
	"bytes"
	"log"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"
)

func TestInit(t *testing.T) {
	tmp := t.TempDir()

	reg := NewRegistry(tmp)
	err := reg.Init()

	if err != nil {
		t.Fatalf("failed to initialize registry: %v", err)
	}

	// Multiple Init should be ok
	err = reg.Init()

	if err != nil {
		t.Fatalf("failed to initialize registry: %v", err)
	}
}

func TestCreateBlobChunked(t *testing.T) {

	tmp := t.TempDir()

	reg := NewRegistry(tmp)
	err := reg.Init()

	if err != nil {
		t.Fatalf("failed to initialize registry: %v", err)
	}

	// create an empty blob

	uid, err := reg.BeginBlob()
	if err != nil {
		t.Fatalf("BeginBlob failed: %v", err)
	}

	fd, err := reg.ResumeBlob(uid, nil)
	if err != nil {
		t.Fatalf("ResumeBlob failed: %v", err)
	}

	fd.Close()

	var info os.FileInfo
	fd, err = reg.ResumeBlob(uid, &info)
	if err != nil {
		t.Fatalf("ResumeBlob failed: %v", err)
	}

	fd.Close()

	// Use a different hasher than the default one
	verify := digest.SHA512.FromString("")

	d, err := reg.FinishBlob(uid, verify)
	if err != nil {
		t.Fatalf("FinishBlob failed: %v", err)
	}

	have := reg.HasBlob(d)
	if !have {
		t.Fatalf("blob '%s' should be present", d.String())
	}
}

func TestPutBlob(t *testing.T) {
	tmp := t.TempDir()

	reg := NewRegistry(tmp)
	err := reg.Init()

	if err != nil {
		t.Fatalf("failed to initialize registry: %v", err)
	}

	buf := bytes.NewBufferString("")
	info, err := reg.PutBlob(buf)

	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	verify := reg.hash.FromString("")

	if verify != info.Digest {
		log.Fatalf("checksum mismatch: %s", info.Digest.String())
	}

	buf = bytes.NewBufferString("otto")
	info, err = reg.PutBlob(buf)

	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	verify = reg.hash.FromString("otto")

	if verify != info.Digest {
		log.Fatalf("checksum mismatch: %s", info.Digest.String())
	}

	check, err := reg.BlobInfo(info.Digest)
	if err != nil {
		t.Fatalf("BlobInfo failed: %v", err)
	}

	if check.Size != 4 {
		t.Fatalf("Invalid blbo size: %d", check.Size)
	}

}
