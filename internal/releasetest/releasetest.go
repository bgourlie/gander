// Package releasetest serves fake Goose releases from a local httptest server, so tests
// never reach the network. Each fake release has small archives for every platform
// holding a shell script named goose that prints a version. It can also serve fake
// releases of gander itself, for "gander self update".
package releasetest

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bgourlie/gander/internal/platform"
	"github.com/bgourlie/gander/internal/release"
	"github.com/bgourlie/gander/internal/version"
)

// File is one entry of a fake archive. A Name ending in "/" is a folder.
type File struct {
	Name string
	Body string
	Mode int64
}

// Release describes one fake release.
type Release struct {
	Tag               string
	Draft, Prerelease bool
	// Targets lists the builds attached; nil means all of platform.Targets.
	Targets []platform.Target
	// Sums attaches a SHA256SUMS file.
	Sums bool
	// NoDigest leaves out the API's digest for each asset.
	NoDigest bool
	// BadDigest gives each asset a digest that doesn't match its bytes.
	BadDigest bool
	// Report is what the fake compiler prints for "goose version"; by default
	// "goose <tag> (runtime test)".
	Report string
	// Files replaces the archive's contents (under its top-level folder).
	Files []File
}

// Script is a fake compiler. It prints $REPORT for "version", and otherwise prints its
// $0 and arguments, one per line, and exits with GOOSE_TEST_EXIT.
const Script = `#!/bin/sh
if [ "$1" = version ]; then echo "$REPORT"; exit 0; fi
echo "argv0=$0"
for a in "$@"; do echo "arg=$a"; done
exit ${GOOSE_TEST_EXIT:-0}
`

// Tree is the default contents of a fake release.
func Tree(report string) []File {
	return []File{
		{Name: "goose", Body: strings.Replace(Script, "$REPORT", report, 1), Mode: 0o755},
		{Name: "stdlib/", Mode: 0o755},
		{Name: "stdlib/std.goose", Body: "// std\n", Mode: 0o644},
		{Name: "docs/", Mode: 0o755},
		{Name: "docs/README.md", Body: "# Goose\n", Mode: 0o644},
	}
}

// TarGz builds a .tar.gz with every file under prefix + "/".
func TarGz(prefix string, files []File) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: prefix + "/", Typeflag: tar.TypeDir, Mode: 0o755})
	for _, f := range files {
		h := &tar.Header{Name: prefix + "/" + f.Name, Mode: f.Mode, Size: int64(len(f.Body)), Typeflag: tar.TypeReg}
		if strings.HasSuffix(f.Name, "/") {
			h.Typeflag, h.Size = tar.TypeDir, 0
		}
		tw.WriteHeader(h)
		tw.Write([]byte(f.Body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// Zip builds a .zip with every file under prefix + "/".
func Zip(prefix string, files []File) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		h := &zip.FileHeader{Name: prefix + "/" + f.Name, Method: zip.Deflate}
		mode := 0o644
		if f.Mode != 0 {
			mode = int(f.Mode)
		}
		h.SetMode(fsMode(f.Name, mode))
		w, _ := zw.CreateHeader(h)
		w.Write([]byte(f.Body))
	}
	zw.Close()
	return buf.Bytes()
}

// Server serves fake releases the way GitHub does: the API under
// /repos/aardappel/goose, and downloads under /aardappel/goose/releases/download that
// redirect to an asset host.
type Server struct {
	*httptest.Server
	Releases []Release

	// RateLimited makes every API request fail as GitHub's rate limit does.
	RateLimited atomic.Bool

	mu           sync.Mutex
	token        string
	assets       map[string][]byte // "<tag>/<name>" → bytes; gander's under "gander-<tag>"
	downloads    map[string]int    // the same keys → completed GETs
	hold         map[string]chan struct{}
	ganderLatest string
}

// New starts a server with these releases, in the order the API lists them (newest
// first, as GitHub does).
func New(t testing.TB, rels ...Release) *Server {
	s := &Server{
		Releases:  rels,
		assets:    map[string][]byte{},
		downloads: map[string]int{},
		hold:      map[string]chan struct{}{},
	}
	for _, r := range rels {
		v := version.MustParse(r.Tag)
		report := r.Report
		if report == "" {
			report = "goose " + r.Tag + " (runtime test)"
		}
		files := r.Files
		if files == nil {
			files = Tree(report)
		}
		var sums strings.Builder
		for _, tg := range targets(r) {
			name := release.AssetName(v, tg)
			prefix := strings.TrimSuffix(name, tg.Archive())
			var data []byte
			if tg.Archive() == ".zip" {
				data = Zip(prefix, files)
			} else {
				data = TarGz(prefix, files)
			}
			s.assets[r.Tag+"/"+name] = data
			sum := sha256.Sum256(data)
			fmt.Fprintf(&sums, "%x *%s\n", sum, name)
		}
		if r.Sums {
			s.assets[r.Tag+"/"+release.SumsName] = []byte(sums.String())
		}
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func targets(r Release) []platform.Target {
	if r.Targets != nil {
		return r.Targets
	}
	return platform.Targets
}

// Client returns a release client for this server, with short timeouts and backoff.
func (s *Server) Client() *release.Client {
	return &release.Client{
		HTTP:         s.Server.Client(),
		API:          s.URL + "/repos/aardappel/goose",
		Releases:     s.URL + "/aardappel/goose/releases",
		UserAgent:    "gander-test",
		StallTimeout: 2 * time.Second,
		Retries:      3,
		Backoff:      10 * time.Millisecond,
	}
}

// AddGander serves a release of gander itself, tagged tag, with these binaries by asset
// name and a SHA256SUMS for them. With latest, <releases>/latest redirects to it.
func (s *Server) AddGander(tag string, latest bool, binaries map[string][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sums strings.Builder
	for name, data := range binaries {
		s.assets[ganderKey(tag)+"/"+name] = data
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(data), name)
	}
	s.assets[ganderKey(tag)+"/"+release.SumsName] = []byte(sums.String())
	if latest {
		s.ganderLatest = tag
	}
}

// GanderDownloads counts the completed downloads of an asset of gander's release tag.
func (s *Server) GanderDownloads(tag, name string) int { return s.Downloads(ganderKey(tag), name) }

func ganderKey(tag string) string { return "gander-" + tag }

// GanderClient returns a client for gander's fake releases, as Client does for Goose's.
func (s *Server) GanderClient() *release.Client {
	c := s.Client()
	c.Releases = s.URL + "/bgourlie/gander/releases"
	return c
}

// Asset returns an asset's bytes.
func (s *Server) Asset(tag, name string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.assets[tag+"/"+name]
}

// Downloads counts the completed downloads of an asset.
func (s *Server) Downloads(tag, name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.downloads[tag+"/"+name]
}

// Hold makes downloads of an asset wait until the returned function is called.
func (s *Server) Hold(tag, name string) (release func()) {
	ch := make(chan struct{})
	s.mu.Lock()
	s.hold[tag+"/"+name] = ch
	s.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/repos/aardappel/goose/"):
		s.serveAPI(w, r, strings.TrimPrefix(p, "/repos/aardappel/goose"))
	case strings.HasPrefix(p, "/aardappel/goose/releases/download/"):
		key := strings.TrimPrefix(p, "/aardappel/goose/releases/download/")
		if s.Asset(dirOf(key), baseOf(key)) == nil {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/assets/"+key+"?signed=1", http.StatusFound)
	case p == "/bgourlie/gander/releases/latest":
		s.mu.Lock()
		latest := s.ganderLatest
		s.mu.Unlock()
		if latest == "" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/bgourlie/gander/releases/tag/"+latest, http.StatusFound)
	case strings.HasPrefix(p, "/bgourlie/gander/releases/download/"):
		key := ganderKey(strings.TrimPrefix(p, "/bgourlie/gander/releases/download/"))
		if s.Asset(dirOf(key), baseOf(key)) == nil {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/assets/"+key+"?signed=1", http.StatusFound)
	case strings.HasPrefix(p, "/assets/"):
		s.serveAsset(w, r, strings.TrimPrefix(p, "/assets/"))
	default:
		http.NotFound(w, r)
	}
}

func dirOf(key string) string  { d, _, _ := strings.Cut(key, "/"); return d }
func baseOf(key string) string { _, b, _ := strings.Cut(key, "/"); return b }

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request, key string) {
	s.mu.Lock()
	data := s.assets[key]
	hold := s.hold[key]
	s.mu.Unlock()
	if data == nil {
		http.NotFound(w, r)
		return
	}
	if hold != nil {
		select {
		case <-hold:
		case <-r.Context().Done():
			return
		}
	}
	http.ServeContent(w, r, baseOf(key), time.Time{}, bytes.NewReader(data))
	s.mu.Lock()
	s.downloads[key]++
	s.mu.Unlock()
}

type apiRelease struct {
	Tag        string     `json:"tag_name"`
	Draft      bool       `json:"draft"`
	Prerelease bool       `json:"prerelease"`
	Assets     []apiAsset `json:"assets"`
}

type apiAsset struct {
	Name   string `json:"name"`
	Size   int    `json:"size"`
	Digest string `json:"digest,omitempty"`
}

func (s *Server) apiRelease(r Release) apiRelease {
	out := apiRelease{Tag: r.Tag, Draft: r.Draft, Prerelease: r.Prerelease, Assets: []apiAsset{}}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, data := range s.assets {
		if dirOf(key) != r.Tag {
			continue
		}
		a := apiAsset{Name: baseOf(key), Size: len(data)}
		sum := sha256.Sum256(data)
		if r.BadDigest {
			sum = sha256.Sum256(append(data, 0))
		}
		if !r.NoDigest {
			a.Digest = "sha256:" + hex.EncodeToString(sum[:])
		}
		out.Assets = append(out.Assets, a)
	}
	return out
}

// RequireToken makes the API answer only requests that carry token, as GitHub does once
// a network's limit without one is used up: requests without a token are rate limited,
// and those with another are refused as bad credentials.
func (s *Server) RequireToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = token
}

func (s *Server) serveAPI(w http.ResponseWriter, r *http.Request, p string) {
	s.mu.Lock()
	token := s.token
	s.mu.Unlock()
	auth := r.Header.Get("Authorization")
	if token != "" && auth != "" && auth != "Bearer "+token {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		return
	}
	if s.RateLimited.Load() || token != "" && auth == "" {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
		http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
		return
	}
	if tag, ok := strings.CutPrefix(p, "/releases/tags/"); ok {
		for _, rel := range s.Releases {
			if rel.Tag == tag {
				json.NewEncoder(w).Encode(s.apiRelease(rel))
				return
			}
		}
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		return
	}
	if p != "/releases" {
		http.NotFound(w, r)
		return
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if perPage <= 0 {
		perPage = 30
	}
	if page <= 0 {
		page = 1
	}
	out := []apiRelease{}
	for i, rel := range s.Releases {
		if i >= (page-1)*perPage && i < page*perPage {
			out = append(out, s.apiRelease(rel))
		}
	}
	json.NewEncoder(w).Encode(out)
}

func fsMode(name string, perm int) os.FileMode {
	if strings.HasSuffix(name, "/") {
		return os.ModeDir | os.FileMode(perm)
	}
	return os.FileMode(perm)
}
