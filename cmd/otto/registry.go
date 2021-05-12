package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type Registry struct {
	Path string

	hash  digest.Algorithm
	blobs string
}

func NewRegistry(path string) *Registry {
	reg := Registry{
		Path: path,
		hash: digest.Canonical,
	}
	return &reg
}

func (reg *Registry) Init() error {
	err := os.MkdirAll(reg.Path, 0700)
	if err != nil {
		return err
	}

	reg.blobs = filepath.Join(reg.Path, "blobs", string(reg.hash))

	err = os.MkdirAll(reg.blobs, 0700)
	if err != nil {
		return err
	}

	incoming := filepath.Join(reg.Path, "incoming")
	err = os.MkdirAll(incoming, 0700)
	if err != nil {
		return err
	}

	return nil
}

func (reg *Registry) PathForBlob(d digest.Digest) string {
	return filepath.Join(reg.Path, "blobs", d.Algorithm().String(), d.Hex())
}

func (reg *Registry) HasBlob(d digest.Digest) bool {
	target := reg.PathForBlob(d)
	_, err := os.Stat(target)

	return err == nil
}

func (reg *Registry) OpenBlob(d digest.Digest, info *os.FileInfo) (*os.File, error) {
	blob := reg.PathForBlob(d)

	fd, err := os.Open(blob)
	if err != nil {
		return nil, err
	}

	if info == nil {
		return fd, nil
	}

	*info, err = fd.Stat()
	if err != nil {
		return nil, err
	}

	return fd, nil
}

func (reg *Registry) BeginBlob() (string, error) {
	uid := uuid.New().String()
	dest := filepath.Join(reg.Path, "incoming", uid)

	fd, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	fd.Close()

	return uid, nil
}

func (reg *Registry) ResumeBlob(uid string, info *os.FileInfo) (*os.File, error) {
	dest := filepath.Join(reg.Path, "incoming", uid)

	fd, err := os.OpenFile(dest, os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	if info == nil {
		return fd, nil
	}

	*info, err = fd.Stat()
	if err != nil {
		return nil, err
	}

	return fd, nil
}

func (reg *Registry) FinishBlob(uid string, verify digest.Digest) (digest.Digest, error) {
	dest := filepath.Join(reg.Path, "incoming", uid)

	fd, err := os.Open(dest)
	if err != nil {
		return "", err
	}

	_, err = fd.Stat()
	if err != nil {
		return "", err
	}

	checksum, err := verify.Algorithm().FromReader(fd)
	if err != nil {
		return "", err
	}

	if checksum != verify {
		return "", fmt.Errorf("checksum mismatch: '%s'", checksum.String())
	}

	if verify.Algorithm() != reg.hash {
		fd.Seek(0, 0)
		checksum, err = reg.hash.FromReader(fd)
		if err != nil {
			return "", err
		}
	}

	target := reg.PathForBlob(checksum)
	err = os.Rename(dest, target)
	if err != nil {
		return "", err
	}

	return checksum, nil
}

func (reg *Registry) PutManifest(manifest v1.Manifest) (digest.Digest, error) {
	data, _ := json.MarshalIndent(manifest, "", "    ")

	checksum := reg.hash.FromBytes(data)

	tmp, err := ioutil.TempDir(reg.Path, ".manifest-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	dest := filepath.Join(tmp, checksum.String())
	file, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, err = file.Write(data)

	if err != nil {
		return "", err
	}

	err = file.Close()

	if err != nil {
		return "", err
	}

	algorithm := checksum.Algorithm().String()
	hexdigest := checksum.Hex()

	target := filepath.Join(reg.Path, "blobs", algorithm, hexdigest)
	err = os.Rename(dest, target)

	if err != nil {
		return "", err
	}

	return checksum, nil
}
