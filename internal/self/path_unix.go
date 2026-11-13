//go:build !windows

package self

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bgourlie/gander/internal/fsutil"
)

// othersFirst ends the note about another goose on PATH: the env script puts gander's
// folder first in every new shell.
const othersFirst = "in new shells, gander's comes first"

// canModifyPath fails, before anything changes, when the startup files can't be found.
func (e *Env) canModifyPath() error {
	if !filepath.IsAbs(e.Getenv("HOME")) {
		return errors.New("HOME isn't set to an absolute path, so gander can't find your shell's startup files; set HOME, or pass --no-modify-path")
	}
	return nil
}

// writeEnv writes the env script, which the startup files source, as does anyone who
// passed --no-modify-path and adds the lines themselves.
func (e *Env) writeEnv() error {
	if err := fsutil.WriteFile(e.envFile(), []byte(envScript(e.Home.BinDir())), 0o644); err != nil {
		return fmt.Errorf("can't write %s: %w", e.envFile(), err)
	}
	return nil
}

func (e *Env) pathByHand() string {
	return fmt.Sprintf("Left PATH alone; to put gander on it, add these lines to your shell's startup file:\n%s", indent(e.block()))
}

// addToPath adds the block to the startup files. A file it can't write is a warning,
// never a failed install: rustup aborts on one (rustup#2040), and home-manager's files
// are read-only.
func (e *Env) addToPath() {
	var added []string
	for _, f := range e.startupFiles(false) {
		did, err := addBlock(f, e.block(), e.envRefs()...)
		if err != nil {
			fmt.Fprintf(e.Stderr, "gander: warning: couldn't update %s (%v); add these lines to it yourself:\n%s", e.tilde(f), err, indent(e.block()))
		} else if did {
			added = append(added, e.tilde(f))
		}
	}
	if e.hasShell("fish") {
		f := e.fishFile()
		err := os.MkdirAll(filepath.Dir(f), 0o755)
		if err == nil {
			err = fsutil.WriteFile(f, []byte(fishScript(e.Home.BinDir())), 0o644)
		}
		if err != nil {
			fmt.Fprintf(e.Stderr, "gander: warning: couldn't write %s (%v)\n", e.tilde(f), err)
		} else {
			added = append(added, e.tilde(f))
		}
	}
	if len(added) > 0 {
		fmt.Fprintf(e.Stderr, "Added gander to PATH in %s\n", strings.Join(added, ", "))
	}
}

// removeFromPath takes gander's blocks out of the startup files, and its fish file away.
func (e *Env) removeFromPath(warn func(format string, args ...any)) {
	if !filepath.IsAbs(e.Getenv("HOME")) {
		fmt.Fprintf(e.Stderr, "Note: HOME isn't set, so gander can't find your shell's startup files; remove its \"%s\" blocks from them yourself\n", blockStart)
		return
	}
	var cleaned []string
	for _, f := range e.startupFiles(true) {
		did, err := removeBlock(f)
		switch {
		case err != nil:
			warn("%v", err)
		case did:
			cleaned = append(cleaned, e.tilde(f))
		}
	}
	if err := os.Remove(e.fishFile()); err == nil {
		cleaned = append(cleaned, e.tilde(e.fishFile()))
	} else if !errors.Is(err, fs.ErrNotExist) {
		warn("couldn't remove %s: %v", e.fishFile(), err)
	}
	if len(cleaned) > 0 {
		fmt.Fprintf(e.Stderr, "Removed gander from %s\n", strings.Join(cleaned, ", "))
	}
}

// NextSteps says how to use gander in the shell that ran the installer, which read its
// startup files before gander changed them.
func (e *Env) NextSteps(bool) string {
	if filepath.Base(e.Getenv("SHELL")) == "fish" {
		return fmt.Sprintf("To use gander in this shell, run:\n    source %s\n", e.tilde(e.fishFile()))
	}
	return fmt.Sprintf("To use gander in this shell, run:\n    . %s\n", e.envRef())
}

// replace renames src over dst. On Unix that works even while dst runs.
func replace(src, dst string) error { return os.Rename(src, dst) }

// discard deletes p, a program in <home>/bin. On Unix a running program, gander included,
// can delete its own file.
func discard(p string) error {
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
