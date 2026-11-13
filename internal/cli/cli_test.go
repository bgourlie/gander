package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/install"
	"github.com/bgourlie/gander/internal/platform"
	"github.com/bgourlie/gander/internal/release"
	"github.com/bgourlie/gander/internal/releasetest"
	"github.com/bgourlie/gander/internal/version"
)

type fixture struct {
	t              *testing.T
	h              home.Home
	dir            string
	vars           map[string]string
	srv            *releasetest.Server
	exe            string // the running gander, for the self commands
	stdout, stderr bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake compiler is a shell script")
	}
	tmp := t.TempDir()
	f := &fixture{
		t:    t,
		h:    home.Home{Dir: filepath.Join(tmp, "home")},
		dir:  filepath.Join(tmp, "proj"),
		vars: map[string]string{},
		srv: releasetest.New(t,
			releasetest.Release{Tag: "v1.1-rc1"},
			releasetest.Release{Tag: "v1.0"},
			releasetest.Release{Tag: "v0.9"},
		),
	}
	os.MkdirAll(f.dir, 0o755)
	return f
}

// run runs gander with args and returns the exit code; output is in f.stdout and f.stderr.
func (f *fixture) run(args ...string) int {
	f.stdout.Reset()
	f.stderr.Reset()
	return Run(context.Background(), &Env{
		Args:       args,
		Stdout:     &f.stdout,
		Stderr:     &f.stderr,
		Getenv:     func(k string) string { return f.vars[k] },
		Home:       f.h,
		WorkingDir: func() (string, error) { return f.dir, nil },
		NewInstaller: func() (*install.Installer, error) {
			return &install.Installer{
				Home: f.h, Client: f.srv.Client(), Target: platform.Target{OS: "macos", Arch: "arm64"},
				Stderr: &f.stderr,
			}, nil
		},
		NewClient:  func() *release.Client { return f.srv.Client() },
		Executable: func() (string, error) { return f.exe, nil },
	})
}

func (f *fixture) mustRun(args ...string) string {
	f.t.Helper()
	if code := f.run(args...); code != 0 {
		f.t.Fatalf("gander %s exited %d\nstderr: %s", strings.Join(args, " "), code, f.stderr.String())
	}
	return f.stdout.String()
}

func (f *fixture) defaultVersion() string {
	v, ok, err := f.h.Default()
	if err != nil || !ok {
		return ""
	}
	return v.String()
}

// firstFields returns the first word of each line, such as the versions "gander list"
// prints, without depending on how the rest of the line is worded.
func firstFields(out string) []string {
	var fs []string
	for line := range strings.Lines(out) {
		if f := strings.Fields(line); len(f) > 0 {
			fs = append(fs, f[0])
		}
	}
	return fs
}

// The first version installed becomes the default; later installs leave it alone, and
// "gander default" moves it, installing the version if needed.
func TestDefaultVersion(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install") // the latest, v1.0
	if f.defaultVersion() != "v1.0" {
		t.Errorf("default = %q after the first install, want v1.0", f.defaultVersion())
	}
	f.mustRun("install", "0.9")
	f.mustRun("install", "v0.9")
	if f.defaultVersion() != "v1.0" {
		t.Errorf("default = %q after installing v0.9, want v1.0", f.defaultVersion())
	}
	v09 := version.MustParse("v0.9")
	if n := f.srv.Downloads("v0.9", release.AssetName(v09, platform.Target{OS: "macos", Arch: "arm64"})); n != 1 {
		t.Errorf("v0.9 downloaded %d times, want 1", n)
	}
	f.mustRun("default", "v1.1-rc1")
	if f.defaultVersion() != "v1.1-rc1" || !f.h.IsInstalled(version.MustParse("v1.1-rc1")) {
		t.Errorf("default = %q after gander default v1.1-rc1", f.defaultVersion())
	}
}

func TestInstallPinned(t *testing.T) {
	f := newFixture(t)
	os.WriteFile(filepath.Join(f.dir, ".goose-version"), []byte("v0.9\n"), 0o644)
	f.mustRun("install")
	if !f.h.IsInstalled(version.MustParse("v0.9")) || f.h.IsInstalled(version.MustParse("v1.0")) {
		t.Error("didn't install just the pinned version")
	}
}

func TestPin(t *testing.T) {
	f := newFixture(t)
	pin := filepath.Join(f.dir, ".goose-version")
	read := func() string { b, _ := os.ReadFile(pin); return string(b) }

	f.mustRun("pin", "v0.9")
	if read() != "v0.9\n" || !f.h.IsInstalled(version.MustParse("v0.9")) {
		t.Errorf("pin v0.9 wrote %q", read())
	}
	f.mustRun("pin", "latest")
	if read() != "v1.0\n" {
		t.Errorf("pin latest wrote %q", read())
	}
	// With no version, pin the one in use: here, from GANDER_GOOSE.
	f.vars["GANDER_GOOSE"] = "v1.1-rc1"
	f.mustRun("pin")
	if read() != "v1.1-rc1\n" {
		t.Errorf("pin wrote %q", read())
	}
	if code := f.run("pin", "v7.0"); code != 1 || read() != "v1.1-rc1\n" {
		t.Errorf("pin of a missing release: exit %d, file %q; want exit 1 and the file unchanged", code, read())
	}
}

func TestListCurrentDir(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install", "v1.0")
	f.mustRun("install", "v0.9")
	pin := filepath.Join(f.dir, ".goose-version")
	os.WriteFile(pin, []byte("v0.9\n"), 0o644)

	if got := firstFields(f.mustRun("list")); !slices.Equal(got, []string{"v1.0", "v0.9"}) {
		t.Errorf("list: %q, want the installed versions, newest first", got)
	}
	if got := firstFields(f.mustRun("list", "--available")); !slices.Equal(got, []string{"v1.1-rc1", "v1.0", "v0.9"}) {
		t.Errorf("list --available: %q, want every release, newest first", got)
	}

	// Scripts may read current's version, the file that chose it and the compiler path.
	v09 := version.MustParse("v0.9")
	out := f.mustRun("current")
	if got := firstFields(out); len(got) != 2 || got[0] != "v0.9" || got[1] != f.h.Compiler(v09) || !strings.Contains(out, pin) {
		t.Errorf("current:\n%s", out)
	}
	if out := f.mustRun("dir"); out != f.h.VersionDir(v09)+"\n" {
		t.Errorf("dir: %s", out)
	}
	os.WriteFile(pin, []byte("v1.1-rc1\n"), 0o644)
	if code := f.run("dir"); code != 1 || f.stdout.Len() != 0 {
		t.Errorf("dir for a version that isn't installed: exit %d, stdout %q", code, f.stdout.String())
	}
}

func TestUninstall(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install", "v1.0")
	f.mustRun("install", "v0.9")
	f.mustRun("uninstall", "v1.0")
	if f.h.IsInstalled(version.MustParse("v1.0")) || f.defaultVersion() != "" {
		t.Errorf("after uninstalling the default: installed %v, default %q", f.h.IsInstalled(version.MustParse("v1.0")), f.defaultVersion())
	}
	if !f.h.IsInstalled(version.MustParse("v0.9")) {
		t.Error("uninstalled the wrong version")
	}
	if code := f.run("uninstall", "v1.0"); code != 1 {
		t.Errorf("uninstalling it again exited %d, want 1", code)
	}
}

// "self install" installs the latest Goose as the default when none is set, and with
// --no-modify-path touches no startup file. "self uninstall" asks first, so with no
// terminal it needs -y.
func TestSelfCommands(t *testing.T) {
	f := newFixture(t)
	user := filepath.Join(filepath.Dir(f.h.Dir), "user")
	os.MkdirAll(user, 0o755)
	f.vars["HOME"] = user
	f.exe = filepath.Join(filepath.Dir(f.h.Dir), "download", "gander")
	os.MkdirAll(filepath.Dir(f.exe), 0o755)
	os.WriteFile(f.exe, []byte("#!/bin/sh\n"), 0o755)

	f.mustRun("self", "install", "--no-modify-path")
	if f.defaultVersion() != "v1.0" || !f.h.IsInstalled(version.MustParse("v1.0")) {
		t.Errorf("default after self install: %q, want the latest, v1.0, installed", f.defaultVersion())
	}
	if entries, _ := os.ReadDir(user); len(entries) != 0 {
		t.Errorf("--no-modify-path wrote %v in HOME", entries)
	}

	f.exe = filepath.Join(f.h.BinDir(), "gander")
	if code := f.run("self", "uninstall"); code != 2 {
		t.Errorf("self uninstall with no terminal and no -y: exit %d, want 2", code)
	}
	f.mustRun("self", "uninstall", "-y")
	if _, err := os.Stat(f.h.Dir); err == nil {
		t.Error("gander's home is still there")
	}
}

// doctor exits 1 when it finds a problem: here nothing is installed and goose isn't on
// PATH.
func TestDoctorExitCode(t *testing.T) {
	f := newFixture(t)
	if code := f.run("doctor"); code != 1 {
		t.Errorf("doctor with nothing installed: exit %d, want 1\nstdout: %s", code, f.stdout.String())
	}
}
