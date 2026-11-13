package self

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/platform"
	"github.com/bgourlie/gander/internal/release"
	"github.com/bgourlie/gander/internal/releasetest"
	"github.com/bgourlie/gander/internal/version"
)

// fakeGander is a gander binary that only answers "version".
func fakeGander(v string) []byte {
	return []byte("#!/bin/sh\necho \"gander " + v + "\"\n")
}

type fixture struct {
	user string // HOME
	vars map[string]string
	env  Env
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake binaries are shell scripts")
	}
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	f := &fixture{user: filepath.Join(tmp, "user"), vars: map[string]string{}}
	os.MkdirAll(f.user, 0o755)
	f.vars["HOME"] = f.user
	f.env = Env{
		Home:   home.Home{Dir: filepath.Join(f.user, ".gander")},
		Getenv: func(k string) string { return f.vars[k] },
		Stderr: &bytes.Buffer{},
	}
	return f
}

func read(p string) string { b, _ := os.ReadFile(p); return string(b) }

func TestInstallAndUninstall(t *testing.T) {
	f := newFixture(t)
	tmp := filepath.Dir(f.user)
	other := filepath.Join(tmp, "brew", "bin")
	os.MkdirAll(other, 0o755)
	os.WriteFile(filepath.Join(other, "goose"), []byte("#!/bin/sh\n"), 0o755)
	f.vars["SHELL"] = "/bin/zsh"
	f.vars["PATH"] = other
	f.vars["GITHUB_PATH"] = filepath.Join(tmp, "github_path")
	bashrc := filepath.Join(f.user, ".bashrc")
	os.WriteFile(bashrc, []byte("alias ll='ls -l'\n"), 0o644)

	download := filepath.Join(tmp, "download", "gander")
	os.MkdirAll(filepath.Dir(download), 0o755)
	os.WriteFile(download, fakeGander("v0.1.0"), 0o755)
	f.env.Exe = download
	if err := f.env.Install(true); err != nil {
		t.Fatal(err)
	}
	gander, goose := f.env.gander(), f.env.goose()
	if read(gander) != read(download) || !sameFile(gander, goose) {
		t.Fatal("bin/gander isn't the download, or bin/goose isn't the same file")
	}
	for _, name := range []string{".profile", ".bashrc", ".zshenv", ".zshrc"} {
		if n := strings.Count(read(filepath.Join(f.user, name)), blockStart); n != 1 {
			t.Errorf("%s has %d blocks, want 1", name, n)
		}
	}
	if exists(filepath.Join(f.user, ".bash_profile")) {
		t.Error("created .bash_profile, which would hide .profile")
	}
	if got := read(f.vars["GITHUB_PATH"]); got != f.env.Home.BinDir()+"\n" {
		t.Errorf("GITHUB_PATH holds %q", got)
	}
	// A login shell that reads .profile finds gander first.
	cmd := exec.Command("/bin/sh", "-c", `. "$HOME/.profile"; printf '%s' "$PATH"`)
	cmd.Env = []string{"HOME=" + f.user, "PATH=/usr/bin:/bin"}
	if out, err := cmd.Output(); err != nil || !strings.HasPrefix(string(out), f.env.Home.BinDir()+":") {
		t.Errorf("PATH after .profile: %q, %v", out, err)
	}

	// Run again from the installed copy, with goose broken: one block each, goose fixed.
	os.Remove(goose)
	os.WriteFile(goose, []byte("stale"), 0o755)
	f.env.Exe = gander
	if err := f.env.Install(true); err != nil {
		t.Fatal(err)
	}
	if !sameFile(gander, goose) || strings.Count(read(bashrc), blockStart) != 1 {
		t.Error("a second install didn't repair goose, or added another block")
	}

	os.MkdirAll(f.env.Home.VersionDir(version.MustParse("v1.0")), 0o755)
	f.env.Home.SetDefault(version.MustParse("v1.0"))
	os.WriteFile(filepath.Join(f.env.Home.Dir, "notes.txt"), []byte("mine"), 0o644)
	if err := f.env.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if read(bashrc) != "alias ll='ls -l'\n" || strings.Contains(read(filepath.Join(f.user, ".zshrc")), blockStart) {
		t.Error("the startup files still have gander's block")
	}
	left, _ := os.ReadDir(f.env.Home.Dir)
	if len(left) != 1 || left[0].Name() != "notes.txt" {
		t.Errorf("left in gander's home: %v, want only the user's notes.txt", left)
	}
}

// Only the copy in gander's home updates or uninstalls itself.
func TestOnlyTheHomeCopy(t *testing.T) {
	f := newFixture(t)
	os.MkdirAll(f.env.Home.BinDir(), 0o755)
	os.WriteFile(f.env.gander(), fakeGander("v0.1.0"), 0o755)
	elsewhere := filepath.Join(f.user, "go", "bin", "gander")
	os.MkdirAll(filepath.Dir(elsewhere), 0o755)
	os.WriteFile(elsewhere, fakeGander("v0.1.0"), 0o755)
	f.env.Exe = elsewhere

	u := &Updater{Env: f.env, Current: "v0.1.0"}
	for name, err := range map[string]error{"update": u.Update(context.Background(), version.Version{}), "uninstall": f.env.Uninstall()} {
		if err == nil || !strings.Contains(err.Error(), elsewhere) {
			t.Errorf("%s from %s: %v, want an error naming it", name, elsewhere, err)
		}
	}
	if !exists(f.env.gander()) {
		t.Error("the installed gander was removed")
	}
}

func TestUpdate(t *testing.T) {
	target := platform.Target{OS: "macos", Arch: "arm64"}
	asset := release.GanderAsset(target)
	setup := func(t *testing.T, rels ...func(*releasetest.Server)) (*fixture, *Updater, *releasetest.Server) {
		f := newFixture(t)
		os.MkdirAll(f.env.Home.BinDir(), 0o755)
		os.WriteFile(f.env.gander(), fakeGander("v0.1.0"), 0o755)
		os.Link(f.env.gander(), f.env.goose())
		f.env.Exe = f.env.gander()
		srv := releasetest.New(t)
		for _, r := range rels {
			r(srv)
		}
		return f, &Updater{Env: f.env, Client: srv.GanderClient(), Target: target}, srv
	}
	rel := func(tag string, latest bool, binary []byte) func(*releasetest.Server) {
		return func(s *releasetest.Server) { s.AddGander(tag, latest, map[string][]byte{asset: binary}) }
	}
	staged := func(f *fixture) []string {
		m, _ := filepath.Glob(filepath.Join(f.env.Home.BinDir(), ".g*-new-*"))
		return m
	}
	ctx := context.Background()

	// A build that isn't a release, as from "go build", updates to the latest.
	f, u, srv := setup(t, rel("v0.2.0", true, fakeGander("v0.2.0")))
	u.Current = "v0.0.0-20261112000000-abcdef123456+dirty"
	if err := u.Update(ctx, version.Version{}); err != nil {
		t.Fatal(err)
	}
	if read(f.env.gander()) != string(fakeGander("v0.2.0")) || !sameFile(f.env.gander(), f.env.goose()) || staged(f) != nil {
		t.Error("after the update, gander isn't v0.2.0, goose isn't the same file, or staging files are left")
	}

	// Up to date: nothing downloaded, but a broken goose is repaired.
	u.Current = "v0.2.0"
	os.Remove(f.env.goose())
	if err := u.Update(ctx, version.Version{}); err != nil || srv.GanderDownloads("v0.2.0", asset) != 1 || !sameFile(f.env.gander(), f.env.goose()) {
		t.Errorf("when up to date: %v, %d downloads, goose same file %v", err, srv.GanderDownloads("v0.2.0", asset), sameFile(f.env.gander(), f.env.goose()))
	}

	// Never an implicit downgrade; a named version may go either way.
	f, u, srv = setup(t, rel("v0.2.0", true, fakeGander("v0.2.0")))
	u.Current = "v0.3.0"
	if err := u.Update(ctx, version.Version{}); err != nil || srv.GanderDownloads("v0.2.0", asset) != 0 {
		t.Errorf("newer than latest: %v, %d downloads", err, srv.GanderDownloads("v0.2.0", asset))
	}
	if err := u.Update(ctx, version.MustParse("v0.2.0")); err != nil || read(f.env.gander()) != string(fakeGander("v0.2.0")) {
		t.Errorf("named older version: %v", err)
	}

	// A new binary that doesn't run as the right version changes nothing.
	f, u, _ = setup(t, rel("v0.2.0", true, fakeGander("v9.9")))
	u.Current = "v0.1.0"
	if err := u.Update(ctx, version.Version{}); err == nil || read(f.env.gander()) != string(fakeGander("v0.1.0")) || staged(f) != nil {
		t.Errorf("a binary reporting the wrong version: %v, or gander changed or staging left", err)
	}

	// A release with no build for this platform is an error naming the asset.
	f, u, _ = setup(t, func(s *releasetest.Server) { s.AddGander("v0.2.0", true, map[string][]byte{"gander-linux-x64": nil}) })
	u.Current = "v0.1.0"
	if err := u.Update(ctx, version.Version{}); err == nil || !strings.Contains(err.Error(), asset) || staged(f) != nil {
		t.Errorf("no build: %v", err)
	}
}
