package main

import (
	"encoding/json"
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

	"github.com/gicmo/otto/internal/container"
	"github.com/gicmo/otto/internal/ostree"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type Server struct {
	root string

	oci  *container.Registry
	repo *ostree.Repo
}

func NewServer(root string) *Server {
	return &Server{
		root: root,
		oci:  container.NewRegistry(filepath.Join(root, "oci")),
		repo: ostree.NewRepo(filepath.Join(root, "ostree", "repo")),
	}
}

func (server *Server) Init() error {
	err := server.oci.Init()
	if err != nil {
		return fmt.Errorf("failed to init registry: %v", err)
	}

	err = server.repo.Init(ostree.ARCHIVE)

	if err != nil {
		return fmt.Errorf("failed to init ostree repo: %w", err)
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

	info, err := server.oci.BlobInfo(d)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Blob does not exist", http.StatusNotFound)
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", d.String())

	w.WriteHeader(http.StatusOK)
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
			http.Error(w, "Blob does not exist", http.StatusNotFound)
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	defer fd.Close()

	w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", d.String())

	_, err = io.CopyN(w, fd, fi.Size())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (server *Server) BeginUpload(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")

	uid, err := server.oci.BeginBlob()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rh := httpRange{0, 0}

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repo, uid))
	w.Header().Set("Content-Range", rh.contentRange(0))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Upload-UUID", uid)

	w.WriteHeader(http.StatusAccepted)
}

func (server *Server) UploadChunked(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uuid")
	repo := chi.URLParam(r, "repo")

	var fi os.FileInfo

	fd, err := server.oci.ResumeBlob(uid, &fi)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rawRange := r.Header.Get("Content-Range")
	if rawRange != "" {

		ranges, err := parseRange(rawRange, fi.Size())
		if err != nil || len(ranges) != 1 {
			http.Error(w, "Invalid range header", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		_, err = fd.Seek(ranges[0].start, 0)
		if err != nil {
			http.Error(w, "Invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
	}

	n, err := io.CopyN(fd, r.Body, r.ContentLength)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Printf("Wrote %d (%d) bytes to %s\n", n, r.ContentLength, uid)

	fi, err = fd.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repo, uid))
	w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", fi.Size()))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Upload-UUID", uid)

	w.WriteHeader(http.StatusAccepted)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", repo, checksum.String()))
	w.WriteHeader(http.StatusCreated)
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

	if ct != v1.MediaTypeImageManifest {
		http.Error(w, fmt.Sprintf("Invalid content type: %s", ct), http.StatusBadRequest)
		return
	}

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
	layer_str := m.Annotations["org.osbuild.ostree.layer"]

	layer_nr, err := strconv.Atoi(layer_str)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cid, err := server.ImportCommitFromImage(commit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", repo, d.String()))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", d.String())
	w.Header().Set("OSTree-Commit-id", cid)

	w.WriteHeader(http.StatusCreated)
}

func (server *Server) GetManifest(w http.ResponseWriter, r *http.Request) {
	repo := chi.URLParam(r, "repo")
	reference := chi.URLParam(r, "reference")

	d := MustParseDigest(reference, w)
	if d == "" {
		return
	}

	for k, v := range r.Header {
		fmt.Printf("%s: %s\n", k, v)
	}

	fmt.Printf("repo: '%s', digest: '%s'\n", repo, d.String())

	var fi os.FileInfo
	fd, err := server.oci.ReadManifest(d, &fi)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Blob does not exist", http.StatusNotFound)
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// header
	w.Header().Set("Docker-Content-Digest", d.String())
	w.Header().Set("Content-Type", v1.MediaTypeImageManifest)

	//body
	_, err = io.CopyN(w, fd, fi.Size())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (server *Server) ImportCommitFromImage(ci CommitInfo) (string, error) {

	blob := server.oci.PathForBlob(ci.layer)

	tmp, err := ioutil.TempDir(server.root, ".import-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	fmt.Printf("Extracting tarball\n")
	cmd := exec.Command("tar", "-x", "--auto-compress", "-f", blob, "-C", tmp)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err = cmd.Run()

	if err != nil {
		return "", fmt.Errorf("could not extract layer: %v", err)
	}

	source := filepath.Join(tmp, strings.TrimLeft(ci.repo, "/"))

	fmt.Printf("Pulling commit (%s) into repo\n", ci.ref)
	err = server.repo.PullLocal(source, ci.ref)
	if err != nil {
		return "", fmt.Errorf("could not pull commit: %w", err)
	}

	cid, err := server.repo.RevParse(ci.ref)

	if err != nil {
		return "", err
	}

	fmt.Printf("Pulled %s\n", cid)
	err = server.repo.UpdateSummary()

	if err != nil {
		return "", err
	}

	return cid, nil
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

	cfg := OttoConfig{
		Root: "/srv/otto",
		Addr: ":3000",
	}

	cfg.TLS.Cert = "/etc/otto/server-crt.pem"
	cfg.TLS.Key = "/etc/otto/server-key.pem"

	err := cfg.LoadConfig("/etc/otto/otto.toml")
	if err != nil {
		log.Fatalf("Failed to read configuration: %v", err)
	}

	server := NewServer(cfg.Root)
	err = server.Init()

	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	// Setup routes
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte("nothing to see here"))
		fmt.Printf("i/o error: %v", err)
	})

	OstreeServer(r, "/ostree/repo", server.repo.Path())

	r.Head("/v2/{repo}/blobs/{digest}", server.HeadBlob)
	r.Get("/v2/{repo}/blobs/{digest}", server.GetBlob)

	r.Post("/v2/{repo}/blobs/uploads/", server.BeginUpload)
	r.Patch("/v2/{repo}/blobs/uploads/{uuid}", server.UploadChunked)
	r.Put("/v2/{repo}/blobs/uploads/{uuid}", server.UploadFinish)
	r.Put("/v2/{repo}/manifests/{reference}", server.UploadManifest)
	r.Get("/v2/{repo}/manifests/{reference}", server.GetManifest)

	r.Get("/v2/", func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte("nothing to see here"))
		fmt.Printf("i/o error: %v", err)
	})

	err = http.ListenAndServeTLS(cfg.Addr, cfg.TLS.Cert, cfg.TLS.Key, r)
	if err != nil {
		log.Fatalf("Failed to server: %v", err)
	}
}
