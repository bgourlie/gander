//go:build unix

package proxy

import (
	"fmt"
	"os"
	"syscall"

	"github.com/bgourlie/gander/internal/platform"
)

// run replaces the proxy with the compiler, so signals, the exit status and the terminal
// all belong to goose. argv[0] is the absolute path; Goose finds its stdlib from the
// OS's record of its executable, not argv[0], and the environment is passed unchanged.
func run(path string, args []string) int {
	err := syscall.Exec(path, append([]string{path}, args...), os.Environ())
	fmt.Fprintf(os.Stderr, "gander: can't run %s: %v\n", path, platform.ExplainExecError(path, err))
	return 1
}
