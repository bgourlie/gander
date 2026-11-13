// Package doctor checks that the Goose version in use can build a program here, and
// says what to do about anything in the way: a version that isn't installed, another
// goose ahead of gander's on PATH, variables that override an install, the OS
// minimums, and the C compiler.
package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bgourlie/gander/internal/fsutil"
	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/platform"
	"github.com/bgourlie/gander/internal/resolve"
)

// Check is the outcome of one check. Name says which: "version", "path",
// "environment", "os", "compiler" or "build".
type Check struct {
	Name    string
	OK      bool
	Summary string
	Detail  []string // what to do, or what a failing step printed
}

// Env is what the checks look at.
type Env struct {
	Home   home.Home
	Getenv func(string) string
	Dir    string // the physical current directory, where a .goose-version is looked for
	DirErr error  // why Dir is unknown, if it is
}

// Run runs the checks in order and passes each to report as it finishes. The compiler
// check needs the version in use installed, and the build check a C compiler; without
// them they don't run.
func Run(ctx context.Context, env Env, report func(Check)) {
	r, err := resolve.Resolve(env.Home, resolve.Request{Getenv: env.Getenv, Dir: env.Dir, DirErr: env.DirErr})
	report(versionCheck(r, err))
	report(pathCheck(env))
	report(environmentCheck(env.Getenv))
	report(osCheck())
	if err != nil || !r.Installed {
		return
	}
	cc := compilerCheck(ctx, r.Path)
	report(cc)
	if cc.OK {
		report(buildCheck(ctx, env, r.Path))
	}
}

func versionCheck(r resolve.Result, err error) Check {
	c := Check{Name: "version"}
	switch {
	case err != nil:
		c.Summary = err.Error()
	case !r.Installed:
		c.Summary = fmt.Sprintf("Goose %s, %s, isn't installed", r.Version, r.Reason())
		c.Detail = []string{fmt.Sprintf("Run \"gander install %s\".", r.Version)}
	default:
		c.OK = true
		c.Summary = fmt.Sprintf("Goose %s, %s", r.Version, r.Reason())
	}
	return c
}

// pathCheck finds the goose a shell or editor would run. Another one ahead of gander's
// runs instead whatever .goose-version says, and quietly.
func pathCheck(env Env) Check {
	c := Check{Name: "path"}
	bin := env.Home.BinDir()
	ours := filepath.Join(bin, home.ExeName("goose"))
	found := fsutil.OnPath(env.Getenv("PATH"), "goose")
	at := slices.IndexFunc(found, func(p string) bool { return sameFile(p, ours) })
	switch {
	case at < 0:
		if len(found) == 0 {
			c.Summary = "goose isn't on PATH"
		} else {
			c.Summary = fmt.Sprintf("goose on PATH is %s, not gander's", found[0])
		}
		c.Detail = []string{fmt.Sprintf("Run \"gander self install\" to put %s on PATH, then open a new terminal.", bin)}
	case at > 0:
		c.Summary = fmt.Sprintf("goose on PATH is %s, ahead of gander's", found[0])
		c.Detail = []string{fmt.Sprintf("Put %s before %s in PATH, or remove %s.", bin, filepath.Dir(found[0]), found[0])}
	case !sameProgram(ours, filepath.Join(bin, home.ExeName("gander"))):
		c.Summary = fmt.Sprintf("%s isn't the gander beside it", ours)
		c.Detail = []string{"Run \"gander self install\" to repair it."}
	default:
		c.OK = true
		c.Summary = fmt.Sprintf("goose on PATH is gander's, in %s", bin)
	}
	return c
}

func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	return err == nil && os.SameFile(fa, fb)
}

// sameProgram also accepts a copy, which "self install" makes where it can't hardlink.
func sameProgram(a, b string) bool {
	if sameFile(a, b) {
		return true
	}
	da, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	db, err := os.ReadFile(b)
	return err == nil && bytes.Equal(da, db)
}

// GOOSE_STDLIB and GOOSE_TCCLIB point every version at one folder instead of the one it
// ships with, so one version's compiler meets another's library.
func environmentCheck(getenv func(string) string) Check {
	c := Check{Name: "environment"}
	var set []string
	for _, v := range []struct{ name, what string }{
		{"GOOSE_STDLIB", "standard library"},
		{"GOOSE_TCCLIB", "TinyCC library"},
	} {
		if val := getenv(v.name); val != "" {
			set = append(set, v.name)
			c.Detail = append(c.Detail, fmt.Sprintf("%s=%s makes every version use that %s instead of its own; unset it.", v.name, val, v.what))
		}
	}
	switch len(set) {
	case 0:
		c.OK = true
		c.Summary = "GOOSE_STDLIB and GOOSE_TCCLIB aren't set"
	case 1:
		c.Summary = set[0] + " is set"
	default:
		c.Summary = strings.Join(set, " and ") + " are set"
	}
	return c
}

func osCheck() Check {
	c := Check{Name: "os"}
	t, err := platform.Detect()
	if err == nil {
		err = platform.Check(t)
	}
	if err != nil {
		c.Summary = err.Error()
		return c
	}
	c.OK = true
	c.Summary = fmt.Sprintf("this machine (%s) meets Goose's minimums", t)
	return c
}

// compilerCheck asks the version which C compiler a build would use. Goose looks for one
// itself (GOOSE_CC, its config, then PATH), so only it can say.
func compilerCheck(ctx context.Context, compiler string) Check {
	c := Check{Name: "compiler"}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	// With no C compiler it exits 1, and still prints the report.
	out, err := exec.CommandContext(ctx, compiler, "toolchain", "--json").Output()
	var tc struct {
		CC *struct {
			Desc, Path, How string
		} `json:"cc"`
		CCError string `json:"cc_error"`
	}
	if jerr := json.Unmarshal(out, &tc); jerr != nil {
		if err == nil {
			err = jerr
		}
		c.Summary = fmt.Sprintf("can't ask %s about its C compiler: %v", compiler, platform.ExplainExecError(compiler, err))
		c.Detail = []string{fmt.Sprintf("Run \"%s toolchain\" to see what it says.", compiler)}
		return c
	}
	if tc.CC == nil {
		c.Summary = "goose finds no C compiler to build with"
		c.Detail = []string{tc.CCError}
		return c
	}
	c.OK = true
	c.Summary = fmt.Sprintf("C compiler: %s, %s (%s)", tc.CC.Desc, tc.CC.Path, tc.CC.How)
	return c
}

const hello = "fn main() {\n    print(\"hello from gander doctor\");\n}\n"

// buildCheck builds and runs hello world, which takes the path every build does:
// Goose's C, the C compiler, the runtime and the linker. The program has a fixed path
// because Goose keeps a build folder in GOOSE_HOME for each source path, so a new path
// each time would leave a new folder each time. It's in versions/, where the
// compilers run from, because /tmp may be mounted noexec.
func buildCheck(ctx context.Context, env Env, compiler string) Check {
	c := Check{Name: "build", Summary: "goose can't build a program"}
	dir := filepath.Join(env.Home.VersionsDir(), ".doctor")
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "hello.goose")
	exe := filepath.Join(dir, home.ExeName("hello"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.Detail = []string{err.Error()}
		return c
	}
	if err := os.WriteFile(src, []byte(hello), 0o644); err != nil {
		c.Detail = []string{err.Error()}
		return c
	}
	build := func(extraEnv ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, compiler, "build", "-o", exe, src)
		cmd.Env = append(os.Environ(), extraEnv...)
		return cmd.CombinedOutput()
	}
	if out, err := build(); err != nil {
		c.Detail = append(outputLines(out, err), sdkHint(ctx, env.Getenv, build)...)
		return c
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(runCtx, exe).CombinedOutput(); err != nil {
		c.Summary = "a program goose built won't run"
		c.Detail = outputLines(out, platform.ExplainExecError(exe, err))
		return c
	}
	c.OK = true
	c.Summary = "goose builds a program, and it runs"
	return c
}

// outputLines is what a failed step printed, or its error if it printed nothing.
func outputLines(out []byte, err error) []string {
	var lines []string
	for line := range strings.Lines(string(out)) {
		lines = append(lines, strings.TrimRight(line, " \t\r\n"))
	}
	if len(lines) == 0 {
		return []string{err.Error()}
	}
	return lines
}
