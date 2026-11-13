// Package cli is gander's own commands: install, pin, default, list, current, dir,
// uninstall, and the self commands that install, update and remove gander.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/bgourlie/gander/internal/buildinfo"
	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/install"
	"github.com/bgourlie/gander/internal/release"
	"github.com/bgourlie/gander/internal/resolve"
	"github.com/bgourlie/gander/internal/term"
)

// Env is what a command runs with.
type Env struct {
	Args           []string // the arguments after "gander"
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	Home           home.Home
	// WorkingDir returns the physical current directory.
	WorkingDir func() (string, error)
	// NewInstaller and NewClient are called only by commands that need them, and
	// NewSelfClient, for gander's own releases, only by "self update".
	NewInstaller  func() (*install.Installer, error)
	NewClient     func() *release.Client
	NewSelfClient func() *release.Client

	// Executable returns the running gander's path, for the self commands.
	Executable func() (string, error)
	// Stdin and StdinIsTerminal are for "self uninstall"'s question.
	Stdin           io.Reader
	StdinIsTerminal bool
}

const usage = `gander installs and switches between versions of the Goose compiler.

Usage:
  gander install [version]     Install a version: with none, the one pinned here,
                               else the latest. The first becomes the default.
  gander pin [version]         Pin this folder to a version in .goose-version,
                               installing it if needed; with none, the one in use.
  gander default <version>     Set the version used where nothing is pinned.
  gander list [--available]    List installed versions, or every release.
  gander current               Show the version in use here, why, and its path.
  gander dir                   Print the folder of the version in use.
  gander uninstall <version>   Remove a version.

  gander self install          Put gander in its home folder and on PATH; the
                               install script runs this. --no-modify-path leaves
                               your shell's startup files alone.
  gander self update           Update gander to its latest release, or to the
                               release named after it.
  gander self uninstall        Remove gander, its PATH setup, and every version;
                               -y skips the question.

A version is a release tag, such as v2026.4, or "latest".

Run Goose itself as "goose". It uses, highest first: a leading +version argument
(goose +v2026.3 build app.goose), GANDER_GOOSE, the nearest .goose-version, or
the default. A version named by one of the first three is installed on first use
unless GANDER_AUTO_INSTALL=0.

Environment:
  GANDER_HOME            gander's folder (default ~/.gander)
  GANDER_GOOSE           the version to use, over any pin
  GANDER_AUTO_INSTALL=0  never install from goose
`

// errUsage is returned for a malformed command line, and errHelp for a command's -h;
// either way the message has been printed.
var (
	errUsage = errors.New("usage")
	errHelp  = errors.New("help")
)

type command struct {
	name string
	run  func(ctx context.Context, env *Env, args []string) error
}

var commands = []command{
	{"install", cmdInstall},
	{"pin", cmdPin},
	{"default", cmdDefault},
	{"list", cmdList},
	{"current", cmdCurrent},
	{"dir", cmdDir},
	{"uninstall", cmdUninstall},
	{"self", cmdSelf},
}

// Run runs one command and returns the exit code.
func Run(ctx context.Context, env *Env) int {
	if len(env.Args) == 0 {
		fmt.Fprint(env.Stderr, usage)
		return 2
	}
	name, args := env.Args[0], env.Args[1:]
	switch {
	case isHelp(name):
		fmt.Fprint(env.Stdout, usage)
		return 0
	case isVersion(name):
		fmt.Fprintln(env.Stdout, "gander", buildinfo.Version())
		return 0
	}
	for _, c := range commands {
		if c.name != name {
			continue
		}
		err := c.run(ctx, env, args)
		switch {
		case err == nil:
			return 0
		case errors.Is(err, errHelp):
			return 0
		case errors.Is(err, errUsage):
			return 2
		case ctx.Err() != nil:
			return 130 // Ctrl-C, as a shell reports a program killed by SIGINT
		default:
			fmt.Fprintf(env.Stderr, "gander: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(env.Stderr, "gander: there's no command %q; run \"gander help\" to see them\n", name)
	return 2
}

func isHelp(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "-help" || arg == "--help"
}
func isVersion(arg string) bool { return arg == "version" || arg == "-version" || arg == "--version" }

// helpOnly reports whether args only ask for usage or gander's version, which need no
// home folder, so a bad GANDER_HOME doesn't hide the help.
func helpOnly(args []string) bool {
	return len(args) == 0 || isHelp(args[0]) || isVersion(args[0])
}

// Main runs gander from the command line and returns the exit code.
func Main() int {
	h, err := home.FromEnv(os.Getenv)
	if err != nil && !helpOnly(os.Args[1:]) {
		fmt.Fprintf(os.Stderr, "gander: %v\n", err)
		return 1
	}
	stderrIsTerminal := term.IsTerminal(os.Stderr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Run(ctx, &Env{
		Args:       os.Args[1:],
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Getenv:     os.Getenv,
		Home:       h,
		WorkingDir: resolve.WorkingDir,
		NewInstaller: func() (*install.Installer, error) {
			return install.New(h, os.Stderr, stderrIsTerminal)
		},
		NewClient: func() *release.Client {
			return release.NewClient(buildinfo.UserAgent())
		},
		NewSelfClient: func() *release.Client {
			c := release.NewClient(buildinfo.UserAgent())
			c.Releases = release.GanderReleases
			return c
		},
		Executable:      os.Executable,
		Stdin:           os.Stdin,
		StdinIsTerminal: term.IsTerminal(os.Stdin),
	})
}

// flags parses a command's flags, printing the command's usage on a mistake. Flags may
// come before or after the operands.
func flags(env *Env, name, synopsis string, args []string, setup func(*flag.FlagSet)) ([]string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if setup != nil {
		setup(fs)
	}
	var operands []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fmt.Fprintf(env.Stdout, "usage: gander %s\n", synopsis)
				return nil, errHelp
			}
			fmt.Fprintf(env.Stderr, "gander %s: %v\nusage: gander %s\n", name, err, synopsis)
			return nil, errUsage
		}
		args = fs.Args()
		if len(args) == 0 {
			return operands, nil
		}
		operands = append(operands, args[0])
		args = args[1:]
	}
}

func operandCount(env *Env, synopsis string, operands []string, lo, hi int) error {
	if len(operands) < lo || len(operands) > hi {
		fmt.Fprintf(env.Stderr, "usage: gander %s\n", synopsis)
		return errUsage
	}
	return nil
}

// inUse resolves the version in use in the current directory, as the proxy does.
func (env *Env) inUse() (resolve.Result, error) {
	dir, err := env.WorkingDir()
	if err != nil {
		return resolve.Result{}, err
	}
	return resolve.Resolve(env.Home, resolve.Request{Getenv: env.Getenv, Dir: dir})
}
