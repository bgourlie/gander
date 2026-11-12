// Package platform names the Goose build for this machine and checks that the machine
// can run it.
package platform

import (
	"fmt"
	"strings"
)

// Target is one of the platforms Goose ships a compiler for, as its asset names spell
// it: "macos-arm64", "linux-x64" and so on.
type Target struct {
	OS, Arch string
}

func (t Target) String() string { return t.OS + "-" + t.Arch }

// Archive is the extension of this target's release archive.
func (t Target) Archive() string {
	if t.OS == "windows" {
		return ".zip"
	}
	return ".tar.gz"
}

// Targets are the platforms Goose ships a compiler for.
var Targets = []Target{
	{"linux", "x64"},
	{"macos", "arm64"},
	{"macos", "x64"},
	{"windows", "x64"},
}

// NoBuildError means Goose ships no compiler for this machine.
type NoBuildError struct {
	Platform string // this machine, such as "linux-arm64"
}

func (e *NoBuildError) Error() string {
	names := make([]string, len(Targets))
	for i, t := range Targets {
		names[i] = t.String()
	}
	return fmt.Sprintf("Goose has no prebuilt compiler for %s; releases include %s. To use Goose here, build it from source: https://github.com/aardappel/goose",
		e.Platform, strings.Join(names, ", "))
}

// Detect returns the Goose build this machine should use. It fails with a
// *NoBuildError when there is none.
func Detect() (Target, error) { return detect() }

// Check reports whether this machine meets the OS minimums for t, before anything is
// downloaded: macOS 13.3, or glibc 2.35 on Linux.
func Check(t Target) error { return check(t) }

// ExplainExecError turns an error from starting the compiler into one that says what to
// do, where the OS's own error would mislead.
func ExplainExecError(path string, err error) error { return explainExecError(path, err) }

// atLeast compares dotted version strings such as "13.3.1" and "13.3" numerically.
// Parts that aren't numbers count as zero.
func atLeast(have, want string) bool {
	h, w := strings.Split(have, "."), strings.Split(want, ".")
	for i := range max(len(h), len(w)) {
		a, b := num(h, i), num(w, i)
		if a != b {
			return a > b
		}
	}
	return true
}

func num(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n := 0
	for _, c := range parts[i] {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
