package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/releasetest"
)

// TestMain lets the test binary stand in for gander: run under the name goose or gander,
// it runs main instead of the tests.
func TestMain(m *testing.M) {
	if isProxy(os.Args[0]) || filepath.Base(os.Args[0]) == "gander" {
		main()
	}
	os.Exit(m.Run())
}

func TestIsProxy(t *testing.T) {
	for argv0, want := range map[string]bool{
		"goose":                        true,
		"/home/me/.gander/bin/goose":   true,
		"GOOSE":                        true,
		`C:\gander\bin\goose.exe`:      runtime.GOOS == "windows",
		"goose.EXE":                    true,
		"Goose.Exe":                    true,
		"gander":                       false,
		"/usr/local/bin/gander":        false,
		"goose-v2026.4":                false,
		"goose.exe.old":                false,
		"mongoose":                     false,
		".exe":                         false,
		"/home/me/goose/bin/gander":    false,
		"/home/me/gander/bin/goose.sh": false,
	} {
		if got := isProxy(argv0); got != want {
			t.Errorf("isProxy(%q) = %v, want %v", argv0, got, want)
		}
	}
}

type e2e struct {
	t    *testing.T
	bin  string // holds goose and gander, hardlinks to the test binary
	home home.Home
	proj string
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the proxy execs, which is Unix only")
	}
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	e := &e2e{t: t, bin: filepath.Join(tmp, "bin"), home: home.Home{Dir: filepath.Join(tmp, "home")}, proj: filepath.Join(tmp, "proj")}
	os.MkdirAll(e.bin, 0o755)
	os.MkdirAll(e.proj, 0o755)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"goose", "gander"} {
		dst := filepath.Join(e.bin, name)
		// A hardlink fails across filesystems, such as a tmpfs /tmp on CI; copy instead,
		// as gander itself falls back to a copy.
		if err := os.Link(self, dst); err != nil {
			data, rerr := os.ReadFile(self)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if err := os.WriteFile(dst, data, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Installed by hand, so these tests need no server.
	for _, tag := range []string{"v1.0", "v0.9"} {
		d := filepath.Join(e.home.VersionsDir(), tag)
		os.MkdirAll(d, 0o755)
		script := strings.Replace(releasetest.Script, "$REPORT", "goose "+tag+" (runtime test)", 1)
		os.WriteFile(filepath.Join(d, "goose"), []byte(script), 0o755)
		os.WriteFile(filepath.Join(d, home.Marker), nil, 0o644)
	}
	return e
}

// run runs bin/<name> in dir and returns stdout, stderr and the exit code.
func (e *e2e) run(dir, name string, env []string, args ...string) (string, string, int) {
	e.t.Helper()
	cmd := exec.Command(filepath.Join(e.bin, name), args...)
	cmd.Dir = dir
	cmd.Env = append([]string{
		"PATH=/usr/bin:/bin",
		"HOME=" + filepath.Dir(e.home.Dir),
		"GANDER_HOME=" + e.home.Dir,
		// Nothing here may reach the network.
		"GANDER_AUTO_INSTALL=0",
		"HTTPS_PROXY=http://127.0.0.1:9",
		"HTTP_PROXY=http://127.0.0.1:9",
	}, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		e.t.Fatal(err)
	}
	return stdout.String(), stderr.String(), code
}

func TestProxyExecs(t *testing.T) {
	e := newE2E(t)
	stdout, stderr, code := e.run(e.proj, "goose", []string{"GOOSE_TEST_EXIT=3"}, "+v1.0", "build", "a b", "", "+v0.9")
	compiler := filepath.Join(e.home.VersionsDir(), "v1.0", "goose")
	want := "argv0=" + compiler + "\narg=build\narg=a b\narg=\narg=+v0.9\n"
	if stdout != want || stderr != "" || code != 3 {
		t.Errorf("goose +v1.0 ... gave exit %d\nstdout:\n%s\nwant:\n%s\nstderr:\n%s", code, stdout, want, stderr)
	}
}

func TestProxyFollowsPinAndDefault(t *testing.T) {
	e := newE2E(t)
	stdout, stderr, code := e.run(e.proj, "goose", nil, "version")
	if stdout != "" || code != 1 || stderr == "" {
		t.Errorf("with nothing selected: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	os.WriteFile(e.home.DefaultFile(), []byte("v1.0\n"), 0o644)
	if stdout, _, _ := e.run(e.proj, "goose", nil, "version"); stdout != "goose v1.0 (runtime test)\n" {
		t.Errorf("default: %q", stdout)
	}
	sub := filepath.Join(e.proj, "src", "pkg")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(e.proj, ".goose-version"), []byte("v0.9\n"), 0o644)
	if stdout, _, _ := e.run(sub, "goose", nil, "version"); stdout != "goose v0.9 (runtime test)\n" {
		t.Errorf("pin: %q", stdout)
	}
	if stdout, _, _ := e.run(sub, "goose", []string{"GANDER_GOOSE=v1.0"}, "version"); stdout != "goose v1.0 (runtime test)\n" {
		t.Errorf("GANDER_GOOSE: %q", stdout)
	}

	stdout, stderr, code = e.run(sub, "goose", nil, "+v2.0", "version")
	if stdout != "" || code != 1 || stderr == "" {
		t.Errorf("a missing version with auto-install off: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestGanderMode(t *testing.T) {
	e := newE2E(t)
	os.WriteFile(filepath.Join(e.proj, ".goose-version"), []byte("v0.9\n"), 0o644)
	stdout, stderr, code := e.run(e.proj, "gander", nil, "current")
	if code != 0 || !strings.HasPrefix(stdout, "v0.9 ") {
		t.Errorf("gander current: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	// A bad GANDER_HOME doesn't hide the help, and is named when it matters.
	stdout, stderr, code = e.run(e.proj, "gander", []string{"GANDER_HOME=relative"}, "help")
	if code != 0 || stdout == "" {
		t.Errorf("gander help with a bad GANDER_HOME: exit %d, stderr %q", code, stderr)
	}
	if _, stderr, code = e.run(e.proj, "gander", []string{"GANDER_HOME=relative"}, "list"); code != 1 || !strings.Contains(stderr, "GANDER_HOME") {
		t.Errorf("gander list with a bad GANDER_HOME: exit %d, stderr %q", code, stderr)
	}
}
