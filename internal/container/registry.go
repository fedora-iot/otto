package container

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type Registry struct {
	Path string

	//default hash algorithm
	hash digest.Algorithm

	// directories
	blobs     string
	incoming  string
	manifests string
}

type BlobInfo struct {
	Digest digest.Digest
	Size   int64
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

	reg.incoming = filepath.Join(reg.Path, "incoming")
	err = os.MkdirAll(reg.incoming, 0700)
	if err != nil {
		return err
	}

	reg.manifests = filepath.Join(reg.Path, "manifests")
	err = os.MkdirAll(reg.incoming, 0700)
	if err != nil {
		return err
	}

	return nil
}

func (reg *Registry) PathForBlob(d digest.Digest) string {
	return filepath.Join(reg.Path, "blobs", d.Algorithm().String(), d.Hex())
}

func (reg *Registry) PathForManifest(d digest.Digest) string {
	return filepath.Join(reg.manifests, d.String())
}

func (reg *Registry) HasBlob(d digest.Digest) bool {
	target := reg.PathForBlob(d)
	_, err := os.Stat(target)

	return err == nil
}

func (reg *Registry) BlobInfo(d digest.Digest) (*BlobInfo, error) {
	target := reg.PathForBlob(d)
	sb, err := os.Stat(target)

	if err != nil {
		return nil, err
	}

	info := BlobInfo{
		Digest: d,
		Size:   sb.Size(),
	}

	return &info, nil
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
	dest := filepath.Join(reg.incoming, uid)

	fd, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	fd.Close()

	return uid, nil
}

func (reg *Registry) ResumeBlob(uid string, info *os.FileInfo) (*os.File, error) {
	dest := filepath.Join(reg.incoming, uid)

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
	dest := filepath.Join(reg.incoming, uid)

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
		_, err = fd.Seek(0, 0)

		if err != nil {
			return "", err
		}

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

func (reg *Registry) PutBlob(data io.Reader) (*BlobInfo, error) {
	digester := reg.hash.Digester()

	fd, err := ioutil.TempFile(reg.incoming, "blob.")
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	out := io.MultiWriter(fd, digester.Hash())

	n, err := io.Copy(out, data)
	if err != nil {
		return nil, err
	}

	err = fd.Close()
	if err != nil {
		return nil, err
	}

	digest := digester.Digest()

	target := reg.PathForBlob(digest)

	err = os.Rename(fd.Name(), target)
	if err != nil {
		return nil, err
	}

	info := BlobInfo{
		Digest: digest,
		Size:   n,
	}

	return &info, nil
}

func (reg *Registry) PutBlobJSON(data interface{}) (*BlobInfo, error) {
	raw, err := json.MarshalIndent(data, "", "    ")
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(raw)
	info, err := reg.PutBlob(buf)

	return info, err
}

func (reg *Registry) PutManifest(manifest v1.Manifest) (digest.Digest, error) {

	for _, layer := range manifest.Layers {
		if !reg.HasBlob(layer.Digest) {
			return "", fmt.Errorf("layer missing: %v", layer.Digest)
		}
	}

	if !reg.HasBlob(manifest.Config.Digest) {
		return "", fmt.Errorf("layer missing: %v", manifest.Config.Digest)
	}

	info, err := reg.PutBlobJSON(manifest)
	if err != nil {
		return "", err
	}

	dir := filepath.Join(reg.manifests, info.Digest.String())
	err = os.Mkdir(dir, 0700)
	if err != nil {
		if os.IsExist(err) {
			return info.Digest, nil
		}
	}

	source := reg.PathForBlob(info.Digest)

	js := filepath.Join(dir, "manifest.json")
	err = os.Link(source, js)

	if err != nil {
		return "", err
	}

	return info.Digest, nil
}
