// Go's archive readers accept "../" and absolute names unless told not to.
//go:debug tarinsecurepath=0
//go:debug zipinsecurepath=0

// Since Go 1.27, SSL_CERT_FILE and SSL_CERT_DIR replace the macOS and Windows system
// verifiers, so someone who exported certifi's bundle for Python would lose the corporate
// certificates in their keychain. Keep the system verifier, as native apps do.
//go:debug x509sslcertoverrideplatform=0

// gander is a version manager for the Goose compiler. Run as "gander" it manages
// versions; run as "goose" (a hardlink or copy of the same binary) it picks a version
// and runs that version's compiler.
package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bgourlie/gander/internal/cli"
	"github.com/bgourlie/gander/internal/proxy"
)

func main() {
	if isProxy(os.Args[0]) {
		os.Exit(proxy.Main())
	}
	os.Exit(cli.Main())
}

// isProxy decides the mode from argv[0], never os.Executable: that resolves symlinks on
// Linux, and cmd passes argv[0] as typed, so the comparison ignores case (rustup#3848).
func isProxy(argv0 string) bool {
	name := filepath.Base(argv0)
	if len(name) > 4 && strings.EqualFold(name[len(name)-4:], ".exe") {
		name = name[:len(name)-4]
	}
	return strings.EqualFold(name, "goose")
}
