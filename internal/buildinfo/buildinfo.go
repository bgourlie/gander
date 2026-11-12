// Package buildinfo reports gander's own version.
package buildinfo

import "runtime/debug"

// version may be set at link time with -ldflags "-X github.com/bgourlie/gander/internal/buildinfo.version=v1.2.3".
var version string

// Version is gander's version: the one set at link time, else the module version the Go
// toolchain recorded, else "dev".
func Version() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// UserAgent identifies gander to GitHub.
func UserAgent() string { return "gander/" + Version() }
