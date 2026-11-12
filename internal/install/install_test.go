package install_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/install"
	"github.com/bgourlie/gander/internal/platform"
	"github.com/bgourlie/gander/internal/release"
	"github.com/bgourlie/gander/internal/releasetest"
	"github.com/bgourlie/gander/internal/version"
)

var macArm = platform.Target{OS: "macos", Arch: "arm64"}

func newInstaller(t *testing.T, srv *releasetest.Server, h home.Home) *install.Installer {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake compiler is a shell script")
	}
	return &install.Installer{Home: h, Client: srv.Client(), Target: macArm, Stderr: io.Discard}
}

func tempHome(t *testing.T) home.Home { return home.Home{Dir: filepath.Join(t.TempDir(), "home")} }

func leftovers(t *testing.T, h home.Home) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(h.VersionsDir(), home.TmpPrefix+"*"))
	return m
}

func TestInstall(t *testing.T) {
	srv := releasetest.New(t, releasetest.Release{Tag: "v1.0"})
	h := tempHome(t)
	in := newInstaller(t, srv, h)
	v := version.MustParse("v1.0")

	if did, err := in.Install(context.Background(), v); err != nil || !did {
		t.Fatalf("Install = %v, %v", did, err)
	}
	if !h.IsInstalled(v) {
		t.Fatal("v1.0 isn't installed")
	}
	if fi, err := os.Stat(h.Compiler(v)); err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("compiler: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.VersionDir(v), "stdlib", "std.goose")); err != nil {
		t.Error(err)
	}
	if m := leftovers(t, h); m != nil {
		t.Errorf("staging folders left behind: %v", m)
	}

	if did, err := in.Install(context.Background(), v); err != nil || did {
		t.Errorf("second Install = %v, %v; want nothing done", did, err)
	}
	if n := srv.Downloads("v1.0", release.AssetName(v, macArm)); n != 1 {
		t.Errorf("%d downloads, want 1", n)
	}
}

// An install either completes and passes its checks, or leaves nothing behind.
func TestInstallVerifies(t *testing.T) {
	v := version.MustParse("v1.0")
	for _, tc := range []struct {
		name string
		rel  releasetest.Release
		ok   bool
	}{
		{"checksum mismatch", releasetest.Release{Tag: "v1.0", BadDigest: true}, false},
		{"compiler reports another version", releasetest.Release{Tag: "v1.0", Report: "goose v9.9 (runtime x)"}, false},
		{"not a compiler", releasetest.Release{Tag: "v1.0", Report: "hello"}, false},
		{"no build for this platform", releasetest.Release{Tag: "v1.0", Targets: []platform.Target{{OS: "linux", Arch: "x64"}}}, false},
		{"no compiler in the archive", releasetest.Release{Tag: "v1.0", Files: []releasetest.File{{Name: "README", Body: "x"}}}, false},
		{"no such release", releasetest.Release{Tag: "v2.0"}, false},
		// Releases before v2026.4 print "goose runtime <hash>" with no version.
		{"compiler prints no version", releasetest.Release{Tag: "v1.0", Report: "goose runtime 4d3da949e563"}, true},
	} {
		h := tempHome(t)
		in := newInstaller(t, releasetest.New(t, tc.rel), h)
		_, err := in.Install(context.Background(), v)
		if (err == nil) != tc.ok || h.IsInstalled(v) != tc.ok {
			t.Errorf("%s: Install = %v, installed %v", tc.name, err, h.IsInstalled(v))
		}
		if _, err := os.Stat(h.VersionDir(v)); !tc.ok && err == nil {
			t.Errorf("%s: left a version folder", tc.name)
		}
		if m := leftovers(t, h); m != nil {
			t.Errorf("%s: left staging folders %v", tc.name, m)
		}
	}
}

func TestInstallWhileRateLimited(t *testing.T) {
	v := version.MustParse("v1.0")
	srv := releasetest.New(t, releasetest.Release{Tag: "v1.0", Sums: true})
	srv.RateLimited.Store(true)
	if _, err := newInstaller(t, srv, tempHome(t)).Install(context.Background(), v); err != nil {
		t.Errorf("with SHA256SUMS: %v", err)
	}

	srv = releasetest.New(t, releasetest.Release{Tag: "v1.0"})
	srv.RateLimited.Store(true)
	var rl *release.RateLimitError
	if _, err := newInstaller(t, srv, tempHome(t)).Install(context.Background(), v); !errors.As(err, &rl) {
		t.Errorf("without SHA256SUMS: %v, want a RateLimitError", err)
	}
}

// An editor and a terminal installing the same version at once download it once.
func TestConcurrentInstalls(t *testing.T) {
	v := version.MustParse("v1.0")
	name := release.AssetName(v, macArm)
	srv := releasetest.New(t, releasetest.Release{Tag: "v1.0"})
	release := srv.Hold("v1.0", name)
	h := tempHome(t)

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		in := newInstaller(t, srv, h)
		wg.Go(func() { _, errs[i] = in.Install(context.Background(), v) })
	}
	time.Sleep(300 * time.Millisecond)
	release()
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("install %d: %v", i, err)
		}
	}
	if n := srv.Downloads("v1.0", name); n != 1 {
		t.Errorf("%d downloads, want 1", n)
	}
}

func TestSweepAndDebris(t *testing.T) {
	v := version.MustParse("v1.0")
	srv := releasetest.New(t, releasetest.Release{Tag: "v1.0"})
	h := tempHome(t)
	in := newInstaller(t, srv, h)

	os.MkdirAll(filepath.Join(h.VersionsDir(), ".tmp-v1.0_123", "tree"), 0o755)
	other := filepath.Join(h.VersionsDir(), ".tmp-v1.0-rc1_456")
	os.MkdirAll(other, 0o755)
	os.MkdirAll(h.VersionDir(v), 0o755)
	os.WriteFile(filepath.Join(h.VersionDir(v), "junk"), nil, 0o644)

	if _, err := in.Install(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	if m := leftovers(t, h); len(m) != 1 || m[0] != other {
		t.Errorf("staging folders after install: %v, want only v1.0-rc1's", m)
	}
	if _, err := os.Stat(filepath.Join(h.VersionDir(v), "junk")); err == nil {
		t.Error("the unfinished install's files are still there")
	}
}
