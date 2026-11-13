package release_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bgourlie/gander/internal/platform"
	"github.com/bgourlie/gander/internal/release"
	"github.com/bgourlie/gander/internal/releasetest"
	"github.com/bgourlie/gander/internal/version"
)

var macArm = platform.Target{OS: "macos", Arch: "arm64"}

func TestListAndLatest(t *testing.T) {
	var rels []releasetest.Release
	// More than one page, newest first as GitHub lists them.
	for i := 120; i >= 1; i-- {
		rels = append(rels, releasetest.Release{Tag: fmt.Sprintf("v1.%d", i), Targets: []platform.Target{macArm}})
	}
	rels = append([]releasetest.Release{
		{Tag: "v3.0", Draft: true},
		{Tag: "v2.0-rc1"},
		{Tag: "v1.999", Prerelease: true},
		{Tag: "v1.9"}, // a backport published after v1.120
	}, rels...)
	srv := releasetest.New(t, rels...)
	c := srv.Client()

	rs, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != len(rels) {
		t.Fatalf("List returned %d releases, want %d", len(rs), len(rels))
	}
	v, ok := release.Latest(rs)
	if !ok || v.String() != "v1.120" {
		t.Errorf("Latest = %v, %v; want v1.120", v, ok)
	}
	if v, err := c.LatestVersion(context.Background()); err != nil || v.String() != "v1.120" {
		t.Errorf("LatestVersion = %v, %v", v, err)
	}
}

func TestAPIErrors(t *testing.T) {
	srv := releasetest.New(t, releasetest.Release{Tag: "v2026.4"})
	c := srv.Client()
	var nf *release.NotFoundError
	if _, err := c.Get(context.Background(), version.MustParse("v2026.9")); !errors.As(err, &nf) {
		t.Errorf("Get of a missing release = %v, want a NotFoundError", err)
	}
	srv.RateLimited.Store(true)
	var rl *release.RateLimitError
	if _, err := c.Get(context.Background(), version.MustParse("v2026.4")); !errors.As(err, &rl) {
		t.Errorf("Get while rate limited = %v, want a RateLimitError", err)
	}
	if _, err := c.LatestVersion(context.Background()); !errors.As(err, &rl) {
		t.Errorf("LatestVersion while rate limited = %v, want a RateLimitError", err)
	}
}

// GITHUB_TOKEN gets answers once the network's limit without a token is used up. A token
// GitHub refuses, such as one for a GitHub Enterprise server, is left out, and a rate
// limit names GITHUB_TOKEN whichever way it went.
func TestToken(t *testing.T) {
	ctx := context.Background()
	srv := releasetest.New(t, releasetest.Release{Tag: "v2026.4"})
	srv.AcceptToken("good")
	c := srv.Client()
	v := version.MustParse("v2026.4")
	c.Token = "for-another-server"
	if _, err := c.Get(ctx, v); err != nil {
		t.Errorf("Get with a refused token: %v", err)
	}

	srv.RateLimited.Store(true)
	var rl *release.RateLimitError
	for _, token := range []string{"", "for-another-server"} {
		c.Token = token
		if _, err := c.Get(ctx, v); !errors.As(err, &rl) || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
			t.Errorf("rate limited, with token %q: %v, want a RateLimitError naming GITHUB_TOKEN", token, err)
		}
	}
	c.Token = "good"
	if _, err := c.LatestVersion(ctx); err != nil {
		t.Errorf("rate limited, with the token: %v", err)
	}
}

func TestChecksum(t *testing.T) {
	ctx := context.Background()
	v := version.MustParse("v2026.4")
	name := release.AssetName(v, macArm)
	for _, tc := range []struct {
		name string
		rel  releasetest.Release
	}{
		{"digest", releasetest.Release{Tag: "v2026.4"}},
		{"SHA256SUMS over a bad digest", releasetest.Release{Tag: "v2026.4", Sums: true, BadDigest: true}},
		{"SHA256SUMS without a digest", releasetest.Release{Tag: "v2026.4", Sums: true, NoDigest: true}},
	} {
		srv := releasetest.New(t, tc.rel)
		c := srv.Client()
		r, err := c.Get(ctx, v)
		if err != nil {
			t.Fatal(err)
		}
		sum, _, err := c.Checksum(ctx, v, r, name, nil)
		want := sha256.Sum256(srv.Asset("v2026.4", name))
		if err != nil || !bytes.Equal(sum, want[:]) {
			t.Errorf("%s: Checksum = %x, %v; want %x", tc.name, sum, err, want)
		}
	}

	// Without the API, only SHA256SUMS can verify a download.
	srv := releasetest.New(t, releasetest.Release{Tag: "v2026.4", Sums: true})
	if _, _, err := srv.Client().Checksum(ctx, v, nil, name, errors.New("rate limited")); err != nil {
		t.Errorf("SHA256SUMS without the API: %v", err)
	}
	srv = releasetest.New(t, releasetest.Release{Tag: "v2026.4"})
	if _, _, err := srv.Client().Checksum(ctx, v, nil, name, errors.New("rate limited")); err == nil {
		t.Error("no SHA256SUMS and no API: want an error")
	}
	srv = releasetest.New(t, releasetest.Release{Tag: "v2026.4", NoDigest: true})
	r, _ := srv.Client().Get(ctx, v)
	if _, _, err := srv.Client().Checksum(ctx, v, r, name, nil); err == nil {
		t.Error("no SHA256SUMS and no digest: want an error")
	}
}

func download(t *testing.T, c *release.Client, url string) ([]byte, []byte, error) {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "dl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sum, n, err := c.Download(context.Background(), url, f, nil)
	if err != nil {
		return nil, nil, err
	}
	data, _ := os.ReadFile(f.Name())
	if int64(len(data)) != n {
		t.Errorf("Download says %d bytes, file has %d", n, len(data))
	}
	return data, sum, nil
}

// A dropped connection and a server error are both retried, and the file that arrives
// is whole.
func TestDownloadRetries(t *testing.T) {
	body := bytes.Repeat([]byte("goose"), 10000)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			w.Write(body[:1000])
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		case 2:
			http.Error(w, "busy", http.StatusBadGateway)
		default:
			w.Write(body)
		}
	}))
	defer srv.Close()
	c := &release.Client{HTTP: srv.Client(), StallTimeout: time.Second, Retries: 3, Backoff: time.Millisecond}
	data, sum, err := download(t, c, srv.URL)
	want := sha256.Sum256(body)
	if err != nil || !bytes.Equal(data, body) || !bytes.Equal(sum, want[:]) {
		t.Errorf("Download = %d bytes, %v; want the whole body", len(data), err)
	}
}

func TestDownloadStall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	c := &release.Client{HTTP: srv.Client(), StallTimeout: 100 * time.Millisecond, Retries: 1, Backoff: time.Millisecond}
	start := time.Now()
	if _, _, err := download(t, c, srv.URL); err == nil {
		t.Error("Download succeeded, want a stall error")
	}
	if time.Since(start) > 3*time.Second {
		t.Error("the stall timer didn't fire")
	}
}
