// Package proxy is gander run as "goose": it picks a version, installs it if allowed,
// and hands over to that version's compiler.
//
// The proxy writes nothing to stdout, since "goose lsp" speaks over it. It never looks
// goose up on PATH, and uses the network only to install a version asked for by name.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/install"
	"github.com/bgourlie/gander/internal/resolve"
	"github.com/bgourlie/gander/internal/term"
)

// Env is what the proxy runs with.
type Env struct {
	Args   []string // the arguments after argv[0]
	Getenv func(string) string
	Stderr io.Writer
	Home   home.Home
	Dir    string // the physical current directory
	DirErr error  // why Dir is unknown, if it is

	// NewInstaller is called only when a version needs installing.
	NewInstaller func() (*install.Installer, error)
}

// Prepare picks the version, installs it if that's allowed, and returns the compiler's
// absolute path and the arguments to run it with.
func Prepare(ctx context.Context, env Env) (path string, args []string, err error) {
	args = env.Args
	var plus string
	if len(args) > 0 && strings.HasPrefix(args[0], "+") {
		// "+version" applies to this one command. It is never exported to the child, so
		// it can't leak into nested builds and override their pins (rustup#2647).
		plus, args = args[0][1:], args[1:]
		if plus == "" {
			return "", nil, errors.New(`"+" needs a version after it, such as goose +v2026.4 version`)
		}
	}
	r, err := resolve.Resolve(env.Home, resolve.Request{Plus: plus, Getenv: env.Getenv, Dir: env.Dir, DirErr: env.DirErr})
	if err != nil {
		return "", nil, err
	}
	if !r.Installed {
		if err := autoInstall(ctx, env, r); err != nil {
			return "", nil, err
		}
	}
	if len(args) > 0 && args[0] == "lsp" {
		// An editor starts the language server in its own choice of directory; this line
		// lands in the editor's language server log and says what was picked and why.
		fmt.Fprintf(env.Stderr, "gander: Goose %s (%s): %s\n", r.Version, r.Reason(), r.Path)
	}
	fi, err := os.Stat(r.Path)
	if err != nil {
		return "", nil, fmt.Errorf("Goose %s's compiler is missing (%v); run \"gander uninstall %s\" and then \"gander install %s\"", r.Version, err, r.Version, r.Version)
	}
	if isSelf(fi) {
		return "", nil, fmt.Errorf("%s is gander itself, not Goose's compiler; run \"gander uninstall %s\" and then \"gander install %s\"", r.Path, r.Version, r.Version)
	}
	return r.Path, args, nil
}

// autoInstall installs a version named by +version, GANDER_GOOSE or a pin. A missing
// default is an error, never a reinstall: rustup reinstalled a default right after it
// was uninstalled (rustup#3635).
func autoInstall(ctx context.Context, env Env, r resolve.Result) error {
	if !r.Named() {
		return fmt.Errorf("the default Goose version, %s, isn't installed (it's set in %s); run \"gander install %s\", or \"gander default <version>\" to pick another", r.Version, r.File, r.Version)
	}
	if env.Getenv("GANDER_AUTO_INSTALL") == "0" {
		return fmt.Errorf("Goose %s (%s) isn't installed, and GANDER_AUTO_INSTALL=0 stops goose installing it; run \"gander install %s\"", r.Version, r.Reason(), r.Version)
	}
	fmt.Fprintf(env.Stderr, "gander: installing Goose %s (%s)\n", r.Version, r.Reason())
	in, err := env.NewInstaller()
	if err != nil {
		return err
	}
	if _, err := in.Install(ctx, r.Version); err != nil {
		return fmt.Errorf("can't install Goose %s: %w", r.Version, err)
	}
	return nil
}

// isSelf reports whether fi is this program: a compiler that is really gander would run
// the proxy again, forever.
func isSelf(fi os.FileInfo) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	me, err := os.Stat(self)
	return err == nil && os.SameFile(me, fi)
}

// Main runs the proxy and returns the exit code. On Unix it doesn't return when the
// compiler starts.
func Main() int {
	h, err := home.FromEnv(os.Getenv)
	if err != nil {
		return fail(err)
	}
	// A missing current directory matters only if no +version or GANDER_GOOSE decides.
	dir, dirErr := resolve.WorkingDir()
	stderrIsTerminal := term.IsTerminal(os.Stderr)
	// Ctrl-C during an install cancels it cleanly instead of leaving a half download.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	path, args, err := Prepare(ctx, Env{
		Args:   os.Args[1:],
		Getenv: os.Getenv,
		Stderr: os.Stderr,
		Home:   h,
		Dir:    dir,
		DirErr: dirErr,
		NewInstaller: func() (*install.Installer, error) {
			return install.New(h, os.Stderr, stderrIsTerminal)
		},
	})
	// Read this before stop, which cancels ctx too.
	wasInterrupted := ctx.Err() != nil
	stop()
	if err != nil {
		if wasInterrupted {
			return interrupted
		}
		return fail(err)
	}
	return run(path, args)
}

// interrupted is the exit code for Ctrl-C, as a shell reports a program killed by SIGINT.
const interrupted = 130

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "gander: %v\n", err)
	return 1
}
