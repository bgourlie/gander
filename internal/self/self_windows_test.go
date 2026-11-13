package self

import (
	"bytes"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/version"
)

// TestMain lets a copy of the test binary stand in for a running gander or goose.
func TestMain(m *testing.M) {
	if os.Getenv("GANDER_TEST_SLEEP") == "1" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// sleep runs the program at path until the test ends.
func sleep(t *testing.T, path string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(path)
	cmd.Env = append(os.Environ(), "GANDER_TEST_SLEEP=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd
}

// On Windows, gander's PATH is the user's Path in the registry, and its programs can't be
// replaced or deleted while they run: self install, an update and self uninstall all
// work around that, and uninstalling refuses while a version's compiler runs.
func TestWindowsSelf(t *testing.T) {
	// A key of its own instead of the user's real environment.
	userEnvKey = `Software\gander-test-` + rand.Text()
	t.Cleanup(func() {
		registry.DeleteKey(registry.CURRENT_USER, userEnvKey)
		userEnvKey = "Environment"
	})
	k, _, err := registry.CreateKey(registry.CURRENT_USER, userEnvKey, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	const others = `%USERPROFILE%\tools;C:\other`
	k.SetExpandStringValue("Path", others)
	path := func() string {
		v, typ, err := k.GetStringValue("Path")
		if err != nil || typ != registry.EXPAND_SZ {
			t.Fatalf("Path: %v, type %d", err, typ)
		}
		return v
	}

	// What uninstall can't delete goes to the temp folder; keep that inside the test's.
	t.Setenv("TMP", t.TempDir())
	tmp := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e := &Env{
		Home:   home.Home{Dir: filepath.Join(tmp, "gander")},
		Exe:    exe,
		Getenv: func(string) string { return "" },
		Stderr: &bytes.Buffer{},
	}
	bin := e.Home.BinDir()

	for range 2 {
		if err := e.Install(true); err != nil {
			t.Fatal(err)
		}
	}
	if got := path(); got != bin+";"+others {
		t.Errorf("after installing twice, Path is %q", got)
	}
	e.Exe = e.gander()

	// Uninstalling waits for a version's compiler to stop.
	v1 := filepath.Join(e.Home.VersionsDir(), "v1.0")
	os.MkdirAll(v1, 0o755)
	os.WriteFile(filepath.Join(v1, home.Marker), nil, 0o644)
	// A copy: a link would share the file of the running test binary.
	if err := copyFile(exe, filepath.Join(v1, "goose.exe")); err != nil {
		t.Fatal(err)
	}
	compiler := sleep(t, filepath.Join(v1, "goose.exe"))
	if err := e.Uninstall(); err == nil || !strings.Contains(err.Error(), "v1.0") {
		t.Errorf("uninstalling with v1.0's compiler running: %v", err)
	}
	if !strings.HasPrefix(path(), bin+";") || !e.Home.IsInstalled(version.MustParse("v1.0")) {
		t.Error("a refused uninstall changed something")
	}
	compiler.Process.Kill()
	compiler.Wait()

	// An update replaces gander and goose while they run.
	sleep(t, e.gander())
	newer := filepath.Join(tmp, "newer.exe")
	os.WriteFile(newer, []byte("a newer gander"), 0o755)
	staged, err := e.stageCopy(newer)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.commit(staged); err != nil {
		t.Fatalf("updating while gander runs: %v", err)
	}
	if b, _ := os.ReadFile(e.goose()); string(b) != "a newer gander" || !sameFile(e.goose(), e.gander()) {
		t.Errorf("after the update, goose.exe holds %q", b)
	}

	// Uninstalling removes everything, though the gander that ran it is running.
	if err := e.Uninstall(); err != nil {
		t.Fatalf("%v\n%s", err, e.Stderr)
	}
	if got := path(); got != others {
		t.Errorf("after uninstalling, Path is %q", got)
	}
	if _, err := os.Stat(e.Home.Dir); !os.IsNotExist(err) {
		left, _ := os.ReadDir(bin)
		t.Errorf("%s is still there, holding %v", e.Home.Dir, left)
	}
}
