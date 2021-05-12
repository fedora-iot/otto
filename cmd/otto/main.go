package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	_ "crypto/sha512"

	"github.com/gicmo/otto/internal/registry"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// taken from golang source code fs.go:
// https://golang.org/src/net/http/fs.go
// httpRange specifies the byte range to be sent to the client.
type httpRange struct {
	start, length int64
}

func (r httpRange) contentRange(size int64) string {
	return fmt.Sprintf("bytes %d-%d/%d", r.start, r.start+r.length-1, size)
}

// parseRange parses a Range header string as per RFC 2616.
func parseRange(s string, size int64) ([]httpRange, error) {
	if s == "" {
		return nil, nil // header not present
	}
	const b = "bytes="
	if !strings.HasPrefix(s, b) {
		return nil, errors.New("invalid range")
	}
	var ranges []httpRange
	for _, ra := range strings.Split(s[len(b):], ",") {
		ra = strings.TrimSpace(ra)
		if ra == "" {
			continue
		}
		i := strings.Index(ra, "-")
		if i < 0 {
			return nil, errors.New("invalid range")
		}
		start, end := strings.TrimSpace(ra[:i]), strings.TrimSpace(ra[i+1:])
		var r httpRange
		if start == "" {
			// If no start is specified, end specifies the
			// range start relative to the end of the file.
			i, err := strconv.ParseInt(end, 10, 64)
			if err != nil {
				return nil, errors.New("invalid range")
			}
			if i > size {
				i = size
			}
			r.start = size - i
			r.length = size - r.start
		} else {
			i, err := strconv.ParseInt(start, 10, 64)
			if err != nil || i >= size || i < 0 {
				return nil, errors.New("invalid range")
			}
			r.start = i
			if end == "" {
				// If no end is specified, range extends to end of the file.
				r.length = size - r.start
			} else {
				i, err := strconv.ParseInt(end, 10, 64)
				if err != nil || r.start > i {
					return nil, errors.New("invalid range")
				}
				if i >= size {
					i = size - 1
				}
				r.length = i - r.start + 1
			}
		}
		ranges = append(ranges, r)
	}
	return ranges, nil
}

type Server struct {
	root string

	oci *registry.Registry
}

func NewServer(root string) *Server {
	return &Server{
		root: root,
		oci:  registry.NewRegistry(filepath.Join(root, "oci")),
	}
}

func (server *Server) Init() error {
	err := server.oci.Init()
	if err != nil {
		return fmt.Errorf("failed to init registry: %v", err)
	}
	return nil
}

func MustParseDigest(raw string, w http.ResponseWriter) digest.Digest {

	d, err := digest.Parse(raw)

	if err != nil {
		msg := fmt.Sprintf("Invalid digest: '%s'", raw)
		http.Error(w, msg, http.StatusBadRequest)
		return ""
	}

	return d
}

func MustHaveDigest(w http.ResponseWriter, r *http.Request) digest.Digest {
	raw := chi.URLParam(r, "digest")
	checksum := MustParseDigest(raw, w)
	return checksum
}

func (server *Server) HeadBlob(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")

	d := MustHaveDigest(w, r)
	if d == "" {
		return
	}

	fmt.Printf("repo: '%s', digest: '%s'\n", repo, d.String())

	var fi os.FileInfo
	_, err := server.oci.OpenBlob(d, &fi)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Blob does not exist", 404)
			return
		}

		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", d.String())

	w.WriteHeader(200)
}

func (server *Server) GetBlob(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")

	d := MustHaveDigest(w, r)
	if d == "" {
		return
	}

	fmt.Printf("repo: '%s', digest: '%s'\n", repo, d.String())

	var fi os.FileInfo
	fd, err := server.oci.OpenBlob(d, &fi)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Blob does not exist", 404)
			return
		}

		http.Error(w, err.Error(), 500)
		return
	}

	_, err = io.CopyN(w, fd, fi.Size())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
}

func (server *Server) BeginUpload(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")

	uid, err := server.oci.BeginBlob()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	rh := httpRange{0, 0}

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repo, uid))
	w.Header().Set("Content-Range", rh.contentRange(0))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Upload-UUID", uid)

	w.WriteHeader(202)
}

func (server *Server) UploadChunked(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uuid")
	repo := chi.URLParam(r, "repo")

	var fi os.FileInfo

	fd, err := server.oci.ResumeBlob(uid, &fi)

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	rawRange := r.Header.Get("Content-Range")
	if rawRange != "" {

		ranges, err := parseRange(rawRange, fi.Size())
		if err != nil || len(ranges) != 1 {
			http.Error(w, "Invalid range header", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		fd.Seek(ranges[0].start, 0)
	}

	n, err := io.CopyN(fd, r.Body, r.ContentLength)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	fmt.Printf("Wrote %d (%d) bytes to %s\n", n, r.ContentLength, uid)

	fi, err = fd.Stat()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repo, uid))
	w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", fi.Size()))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Upload-UUID", uid)

	w.WriteHeader(202)
}

func (server *Server) UploadFinish(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uuid")
	repo := chi.URLParam(r, "repo")

	rawDigest := r.URL.Query().Get("digest")
	checksum := MustParseDigest(rawDigest, w)
	if checksum == "" {
		return
	}

	checksum, err := server.oci.FinishBlob(uid, checksum)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", repo, checksum.String()))
	w.WriteHeader(201)
}

type CommitInfo struct {
	repo  string
	ref   string
	layer digest.Digest
}

func (server *Server) UploadManifest(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	reference := chi.URLParam(r, "reference")

	ct := r.Header.Get("Content-Type")
	fmt.Printf("repo: '%s', reference '%s' '%s'\n", repo, reference, ct)

	var m v1.Manifest

	err := json.NewDecoder(r.Body).Decode(&m)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var commit CommitInfo
	commit.repo = m.Annotations["org.osbuild.ostree.repo"]
	commit.ref = m.Annotations["org.osbuild.ostree.ref"]
	layer_nr_str := m.Annotations["org.osbuild.ostree.layer_nr"]

	layer_nr, err := strconv.Atoi(layer_nr_str)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if layer_nr > len(m.Layers) {
		http.Error(w, "Invalid OSTree layer id", http.StatusBadRequest)
		return
	}

	commit.layer = m.Layers[layer_nr].Digest

	if commit.repo == "" || commit.ref == "" {
		http.Error(w, "Manifest does not contain ostree commit", http.StatusBadRequest)
		return
	}

	d, err := server.oci.PutManifest(m)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	cid, err := server.ImportCommit(commit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", repo, d.String()))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", d.String())
	w.Header().Set("OSTree-Commit-id", cid)

	w.WriteHeader(201)
}

func (server *Server) ImportCommit(ci CommitInfo) (string, error) {

	blob := server.oci.PathForBlob(ci.layer)

	tmp, err := ioutil.TempDir(server.root, ".import-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	cmd := exec.Command("tar", "-x", "--auto-compress", "-f", blob, "-C", tmp)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err = cmd.Run()

	if err != nil {
		return "", fmt.Errorf("could not extract layer: %v", err)
	}

	source := filepath.Join(tmp, strings.TrimLeft(ci.repo, "/"))
	target := filepath.Join(server.root, "ostree", "repo")

	cmd = exec.Command("ostree", "pull-local", source, "--repo", target, ci.ref)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err = cmd.Run()

	if err != nil {
		return "", fmt.Errorf("failed to pull ostree ref: %v", err)
	}

	fmt.Printf("rev-parse of %s\n", ci.ref)
	cmd = exec.Command("ostree", "rev-parse", "--repo", target, ci.ref)

	var res bytes.Buffer
	cmd.Stdout = &res

	err = cmd.Run()
	fmt.Printf("rev-parse: %s\n", res.String())

	if err != nil {
		return "", fmt.Errorf("failed to resolve ostree ref '%s': %v", ci.ref, err)
	}

	cmd = exec.Command("ostree", "summary", "-u", "--repo", target, ci.ref)
	err = cmd.Run()

	if err != nil {
		return "", fmt.Errorf("failed to update ostree summary: %v", err)
	}

	return strings.TrimSpace(res.String()), nil
}

func OstreeServer(r chi.Router, public string, repo string) {

	if strings.ContainsAny(public, "{}*") {
		panic("OstreeServer does not permit URL parameters.")
	}

	fs := http.StripPrefix(public, http.FileServer(http.Dir(repo)))

	if public != "/" && public[len(public)-1] != '/' {
		r.Get(public, http.RedirectHandler(public+"/", http.StatusMovedPermanently).ServeHTTP)
		public += "/"
	}

	r.Get(public+"*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.ServeHTTP(w, r)
	}))
}

func main() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("nothing to see here"))
	})

	server := NewServer("/tmp/otto")
	err := server.Init()

	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	repo := filepath.Join(server.root, "ostree", "repo")
	err = os.MkdirAll(repo, 0700)
	if err != nil {
		log.Fatalf("Failed to create dir: %v", err)
	}

	cmd := exec.Command("ostree", "init", "--repo", repo, "--mode", "archive")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err = cmd.Run()

	if err != nil {
		log.Fatalf("Failed to initialize ostree repo: %v", err)
	}

	OstreeServer(r, "/ostree/repo", repo)

	r.Head("/v2/{repo}/blobs/{digest}", server.HeadBlob)
	r.Get("/v2/{repo}/blobs/{digest}", server.GetBlob)

	r.Post("/v2/{repo}/blobs/uploads/", server.BeginUpload)
	r.Patch("/v2/{repo}/blobs/uploads/{uuid}", server.UploadChunked)
	r.Put("/v2/{repo}/blobs/uploads/{uuid}", server.UploadFinish)
	r.Put("/v2/{repo}/manifests/{reference}", server.UploadManifest)

	r.Get("/v2/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	http.ListenAndServeTLS(":3000", "/etc/otto/server-crt.pem", "/etc/otto/server-key.pem", r)
}
