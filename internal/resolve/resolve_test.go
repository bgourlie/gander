package resolve

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/version"
)

type fixture struct {
	h    home.Home
	root string // a project folder with sub/deeper inside it
	env  map[string]string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	tmp := t.TempDir()
	f := &fixture{h: home.Home{Dir: filepath.Join(tmp, "home")}, root: filepath.Join(tmp, "proj"), env: map[string]string{}}
	os.MkdirAll(filepath.Join(f.root, "sub", "deeper"), 0o755)
	return f
}

func (f *fixture) resolve(plus, dir string) (Result, error) {
	return Resolve(f.h, Request{Plus: plus, Getenv: func(k string) string { return f.env[k] }, Dir: dir})
}

func (f *fixture) install(t *testing.T, tag string) {
	t.Helper()
	d := f.h.VersionDir(version.MustParse(tag))
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, home.Marker), nil, 0o644)
}

func TestPriority(t *testing.T) {
	f := newFixture(t)
	deeper := filepath.Join(f.root, "sub", "deeper")

	if _, err := f.resolve("", deeper); !errors.As(err, new(*NoVersionError)) {
		t.Fatalf("with nothing set: %v, want a NoVersionError", err)
	}

	f.h.SetDefault(version.MustParse("v1.0"))
	r, err := f.resolve("", deeper)
	if err != nil || r.Version.String() != "v1.0" || r.Source != Default || r.File != f.h.DefaultFile() || r.Named() {
		t.Fatalf("default: %+v, %v", r, err)
	}

	pin := filepath.Join(f.root, ".goose-version")
	os.WriteFile(pin, []byte("2.0\n"), 0o644)
	r, err = f.resolve("", deeper)
	if err != nil || r.Version.String() != "v2.0" || r.Source != Pin || r.File != pin || !r.Named() {
		t.Fatalf("pin: %+v, %v", r, err)
	}

	// The nearest pin wins.
	nearer := filepath.Join(f.root, "sub", ".goose-version")
	os.WriteFile(nearer, []byte("v2.1\n"), 0o644)
	r, _ = f.resolve("", deeper)
	if r.Version.String() != "v2.1" || r.File != nearer {
		t.Fatalf("nearest pin: %+v", r)
	}

	f.env["GANDER_GOOSE"] = "v3.0"
	r, err = f.resolve("", deeper)
	if err != nil || r.Version.String() != "v3.0" || r.Source != Env {
		t.Fatalf("GANDER_GOOSE: %+v, %v", r, err)
	}

	r, err = f.resolve("v4.0", deeper)
	if err != nil || r.Version.String() != "v4.0" || r.Source != Plus {
		t.Fatalf("+version: %+v, %v", r, err)
	}
	if r.Path != filepath.Join(f.h.Dir, "versions", "v4.0", home.ExeName("goose")) || r.Installed {
		t.Errorf("path %s, installed %v", r.Path, r.Installed)
	}
	f.install(t, "v4.0")
	if r, _ = f.resolve("v4.0", deeper); !r.Installed {
		t.Error("v4.0 should be installed")
	}
}

func TestBrokenSourcesNeverFallThrough(t *testing.T) {
	f := newFixture(t)
	f.h.SetDefault(version.MustParse("v1.0"))
	os.WriteFile(filepath.Join(f.root, ".goose-version"), []byte("v2.0\n"), 0o644)
	bad := filepath.Join(f.root, "sub", ".goose-version")
	os.WriteFile(bad, []byte("stable\n"), 0o644)
	_, err := f.resolve("", filepath.Join(f.root, "sub", "deeper"))
	if err == nil || !strings.Contains(err.Error(), bad) {
		t.Errorf("a bad pin: %v, want an error naming %s", err, bad)
	}

	f.env["GANDER_GOOSE"] = "nightly"
	if _, err := f.resolve("", f.root); err == nil || !strings.Contains(err.Error(), "GANDER_GOOSE") {
		t.Errorf("a bad GANDER_GOOSE: %v", err)
	}
	if _, err := f.resolve("../x", f.root); err == nil || !strings.Contains(err.Error(), "+../x") {
		t.Errorf("a bad +version: %v", err)
	}

	f = newFixture(t)
	os.MkdirAll(f.h.Dir, 0o755)
	os.WriteFile(f.h.DefaultFile(), []byte("garbage"), 0o644)
	if _, err := f.resolve("", f.root); err == nil || !strings.Contains(err.Error(), f.h.DefaultFile()) {
		t.Errorf("a bad default: %v", err)
	}
}

func TestWorkingDirIsPhysical(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	real := filepath.Join(tmp, "real")
	os.MkdirAll(real, 0o755)
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	t.Chdir(link)
	t.Setenv("PWD", link)
	wd, err := WorkingDir()
	if err != nil || wd != real {
		t.Errorf("WorkingDir() = %q, %v; want %q", wd, err, real)
	}
}
