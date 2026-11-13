package doctor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/version"
)

// The fake compiler's answers. Each is a case in a shell script, so one fake can be
// healthy in one part and broken in another.
const (
	toolchainOK     = `toolchain) echo '{"cc": {"desc": "fakecc 1.0", "path": "/usr/bin/fakecc", "how": "on PATH"}}' ;;`
	toolchainNoCC   = `toolchain) echo '{"cc": null, "cc_error": "cannot find a C compiler"}'; exit 1 ;;`
	buildOK         = `build) printf '#!/bin/sh\n' >"$3"; chmod +x "$3" ;;`
	buildFails      = `build) echo "ld: fake link error" >&2; exit 1 ;;`
	buildNeedsSDK   = `build) if [ -z "$SDKROOT" ]; then echo "ld: unknown architecture" >&2; exit 1; fi; printf '#!/bin/sh\n' >"$3"; chmod +x "$3" ;;`
	builtWontRun    = `build) printf '#!/bin/sh\necho fake crash; exit 3\n' >"$3"; chmod +x "$3" ;;`
	fakeVersion     = "v1.0"
	missingVersion  = "v2.0"
	stdlibOverride  = "/somewhere/stdlib"
	otherGooseName  = "other"
	brokenGooseName = "goose is a stale copy"
)

type fixture struct {
	env  Env
	vars map[string]string
	bin  string
}

// newFixture sets up a healthy install: gander in <home>/bin with goose hardlinked
// beside it and first on PATH, and the default version's compiler answering as told.
func newFixture(t *testing.T, toolchain, build string) *fixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake compiler is a shell script")
	}
	tmp := t.TempDir()
	h := home.Home{Dir: filepath.Join(tmp, "home")}
	f := &fixture{vars: map[string]string{}, bin: h.BinDir()}
	f.env = Env{Home: h, Getenv: func(k string) string { return f.vars[k] }, Dir: tmp}
	f.vars["PATH"] = f.bin

	write(t, filepath.Join(f.bin, "gander"), "gander")
	if err := os.Link(filepath.Join(f.bin, "gander"), filepath.Join(f.bin, "goose")); err != nil {
		t.Fatal(err)
	}
	v := version.MustParse(fakeVersion)
	write(t, h.Compiler(v), "#!/bin/sh\ncase $1 in\n"+toolchain+"\n"+build+"\nesac\n")
	write(t, filepath.Join(h.VersionDir(v), home.Marker), "")
	if err := h.SetDefault(v); err != nil {
		t.Fatal(err)
	}
	return f
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// run returns the checks by name. "os" is left out: it reads the real machine.
func (f *fixture) run(t *testing.T) map[string]Check {
	t.Helper()
	checks := map[string]Check{}
	Run(context.Background(), f.env, func(c Check) {
		if _, dup := checks[c.Name]; dup {
			t.Errorf("check %q ran twice", c.Name)
		}
		if c.Name != "os" {
			checks[c.Name] = c
		}
	})
	return checks
}

func failing(checks map[string]Check) []string {
	var names []string
	for name, c := range checks {
		if !c.OK {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func mentions(c Check, s string) bool {
	return strings.Contains(c.Summary, s) || slices.ContainsFunc(c.Detail, func(d string) bool { return strings.Contains(d, s) })
}

func TestHealthy(t *testing.T) {
	f := newFixture(t, toolchainOK, buildOK)
	checks := f.run(t)
	if bad := failing(checks); len(bad) > 0 {
		t.Errorf("failing checks %v on a healthy install: %+v", bad, checks)
	}
	if _, ok := checks["build"]; !ok {
		t.Error("the build check didn't run")
	}
	if _, err := os.Stat(filepath.Join(f.env.Home.VersionsDir(), ".doctor")); err == nil {
		t.Error("the build check left its program behind")
	}
}

// Each problem fails its own check, names what's at fault, and skips the checks that
// depend on it.
func TestProblems(t *testing.T) {
	for _, tc := range []struct {
		name             string
		toolchain, build string
		setup            func(t *testing.T, f *fixture)
		fails            string
		names            string // what the failing check must mention
		skips            []string
	}{
		{
			name: "another goose ahead on PATH", toolchain: toolchainOK, build: buildOK,
			setup: func(t *testing.T, f *fixture) {
				other := filepath.Join(t.TempDir(), otherGooseName)
				write(t, filepath.Join(other, "goose"), "#!/bin/sh\n")
				f.vars["PATH"] = other + string(os.PathListSeparator) + f.bin
			},
			fails: "path", names: otherGooseName,
		},
		{
			name: "goose isn't gander", toolchain: toolchainOK, build: buildOK,
			setup: func(t *testing.T, f *fixture) {
				p := filepath.Join(f.bin, "goose")
				os.Remove(p)
				write(t, p, brokenGooseName)
			},
			fails: "path", names: "self install",
		},
		{
			name: "GOOSE_STDLIB", toolchain: toolchainOK, build: buildOK,
			setup: func(t *testing.T, f *fixture) { f.vars["GOOSE_STDLIB"] = stdlibOverride },
			fails: "environment", names: "GOOSE_STDLIB",
		},
		{
			name: "version not installed", toolchain: toolchainOK, build: buildOK,
			setup: func(t *testing.T, f *fixture) { f.vars["GANDER_GOOSE"] = missingVersion },
			fails: "version", names: missingVersion, skips: []string{"compiler", "build"},
		},
		{
			name: "no C compiler", toolchain: toolchainNoCC, build: buildOK,
			fails: "compiler", names: "cannot find a C compiler", skips: []string{"build"},
		},
		{
			name: "build fails", toolchain: toolchainOK, build: buildFails,
			fails: "build", names: "fake link error",
		},
		{
			name: "built program won't run", toolchain: toolchainOK, build: builtWontRun,
			fails: "build", names: "fake crash",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.toolchain, tc.build)
			if tc.setup != nil {
				tc.setup(t, f)
			}
			checks := f.run(t)
			if bad := failing(checks); !slices.Equal(bad, []string{tc.fails}) {
				t.Fatalf("failing checks = %v, want [%s]: %+v", bad, tc.fails, checks)
			}
			if c := checks[tc.fails]; !mentions(c, tc.names) {
				t.Errorf("the %s check doesn't mention %q: %+v", tc.fails, tc.names, c)
			}
			for _, name := range tc.skips {
				if _, ran := checks[name]; ran {
					t.Errorf("the %s check ran", name)
				}
			}
		})
	}
}

// A Mac whose default SDK the linker can't read: the build check tries the selected
// developer tools' own SDK, and when that builds, says to set SDKROOT.
func TestSDKHint(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	t.Setenv("SDKROOT", "") // the fake compiler sees the real environment
	if err := exec.Command("xcrun", "--sdk", "macosx", "--show-sdk-path").Run(); err != nil {
		t.Skip("no macOS SDK here:", err)
	}
	f := newFixture(t, toolchainOK, buildNeedsSDK)
	c := f.run(t)["build"]
	if c.OK || !mentions(c, "SDKROOT") {
		t.Errorf("build check = %+v, want a failure that mentions SDKROOT", c)
	}
}
