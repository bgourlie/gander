package proxy

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/install"
	"github.com/bgourlie/gander/internal/platform"
	"github.com/bgourlie/gander/internal/releasetest"
	"github.com/bgourlie/gander/internal/version"
)

type fixture struct {
	env       Env
	stderr    bytes.Buffer
	vars      map[string]string
	installs  int
	installer *install.Installer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake compiler is a shell script")
	}
	srv := releasetest.New(t, releasetest.Release{Tag: "v1.0"}, releasetest.Release{Tag: "v0.9"})
	tmp := t.TempDir()
	f := &fixture{vars: map[string]string{}}
	h := home.Home{Dir: filepath.Join(tmp, "home")}
	f.installer = &install.Installer{
		Home: h, Client: srv.Client(), Target: platform.Target{OS: "macos", Arch: "arm64"},
		Stderr: &f.stderr,
	}
	f.env = Env{
		Getenv: func(k string) string { return f.vars[k] },
		Stderr: &f.stderr,
		Home:   h,
		Dir:    filepath.Join(tmp, "proj"),
		NewInstaller: func() (*install.Installer, error) {
			f.installs++
			return f.installer, nil
		},
	}
	os.MkdirAll(f.env.Dir, 0o755)
	return f
}

func (f *fixture) prepare(args ...string) (string, []string, error) {
	f.env.Args = args
	return Prepare(context.Background(), f.env)
}

func TestPlusVersionInstallsAndIsStripped(t *testing.T) {
	f := newFixture(t)
	v := version.MustParse("v1.0")
	path, args, err := f.prepare("+v1.0", "build", "+x", "app.goose")
	if err != nil {
		t.Fatal(err)
	}
	if path != f.env.Home.Compiler(v) || !slices.Equal(args, []string{"build", "+x", "app.goose"}) {
		t.Errorf("Prepare = %s %q", path, args)
	}
	if !f.env.Home.IsInstalled(v) || f.installs != 1 {
		t.Errorf("installed %v after %d installs", f.env.Home.IsInstalled(v), f.installs)
	}
	// Once it's installed, the proxy doesn't ask for an installer, so no network.
	if _, _, err := f.prepare("+v1.0", "version"); err != nil || f.installs != 1 {
		t.Errorf("second run: %v, %d installs", err, f.installs)
	}
	if _, _, err := f.prepare("+", "version"); err == nil {
		t.Error("a bare + should be an error")
	}
}

// Only a version named by +version, GANDER_GOOSE or a pin is installed automatically.
func TestWhenNotToInstall(t *testing.T) {
	f := newFixture(t)
	f.env.Home.SetDefault(version.MustParse("v1.0"))
	_, _, err := f.prepare("version")
	if err == nil || !strings.Contains(err.Error(), "gander install v1.0") || f.installs != 0 {
		t.Errorf("missing default: %v, %d installs; want an error naming the command to run", err, f.installs)
	}
	f.vars["GANDER_AUTO_INSTALL"] = "0"
	if _, _, err := f.prepare("+v1.0", "version"); err == nil || f.installs != 0 {
		t.Errorf("GANDER_AUTO_INSTALL=0: %v, %d installs", err, f.installs)
	}
}

func TestLSPSaysWhatItPicked(t *testing.T) {
	f := newFixture(t)
	if _, _, err := f.prepare("+v1.0", "lsp"); err != nil {
		t.Fatal(err)
	}
	if log := f.stderr.String(); !strings.Contains(log, "v1.0") || !strings.Contains(log, f.env.Home.Compiler(version.MustParse("v1.0"))) {
		t.Errorf("stderr = %q, want the version and compiler path", log)
	}
}

// With the current directory gone, +version and GANDER_GOOSE still work; only a pin
// lookup needs the directory.
func TestMissingDirectory(t *testing.T) {
	f := newFixture(t)
	dirErr := errors.New("can't find the current directory")
	f.env.Dir, f.env.DirErr = "", dirErr
	if _, _, err := f.prepare("+v1.0", "version"); err != nil {
		t.Errorf("+v1.0: %v", err)
	}
	f.vars["GANDER_GOOSE"] = "v1.0"
	if _, _, err := f.prepare("version"); err != nil {
		t.Errorf("GANDER_GOOSE: %v", err)
	}
	delete(f.vars, "GANDER_GOOSE")
	if _, _, err := f.prepare("version"); !errors.Is(err, dirErr) {
		t.Errorf("no version named: %v, want the directory error", err)
	}
}

func TestMissingCompiler(t *testing.T) {
	f := newFixture(t)
	if _, _, err := f.prepare("+v1.0", "version"); err != nil {
		t.Fatal(err)
	}
	os.Remove(f.env.Home.Compiler(version.MustParse("v1.0")))
	if _, _, err := f.prepare("+v1.0", "version"); err == nil || !strings.Contains(err.Error(), "gander uninstall v1.0") {
		t.Errorf("Prepare = %v, want an error naming the command to run", err)
	}
}
