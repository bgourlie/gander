package proxy

import (
	"fmt"
	"os"
)

// Windows has no exec: the proxy must start goose as a child inside a job object, pass
// Ctrl-C and the exit code through, and more. That comes in the Windows milestone.
func run(path string, _ []string) int {
	fmt.Fprintf(os.Stderr, "gander: the goose proxy doesn't run on Windows yet; run %s directly\n", path)
	return 1
}
