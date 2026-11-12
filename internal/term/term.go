// Package term tells whether a file is a terminal.
package term

import "os"

// IsTerminal reports whether f is a terminal.
func IsTerminal(f *os.File) bool { return isTerminal(f.Fd()) }
