package self

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Uninstall removes gander: the blocks in the startup files, then only the names gander
// created in its home, the installed versions among them, then the home itself if
// that leaves it empty. Deleting the whole folder would delete whatever else is there:
// a user who had linked ~/.cargo/bin to ~/bin lost ~/bin to rustup (rustup#1072).
// Every step can be repeated, so running it again finishes a removal that failed.
func (e *Env) Uninstall() error {
	if err := e.owned("remove"); err != nil {
		return err
	}
	var problems int
	warn := func(format string, args ...any) {
		problems++
		fmt.Fprintf(e.Stderr, "gander: warning: "+format+"\n", args...)
	}

	if filepath.IsAbs(e.Getenv("HOME")) {
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
	} else {
		fmt.Fprintf(e.Stderr, "Note: HOME isn't set, so gander can't find your shell's startup files; remove its \"%s\" blocks from them yourself\n", blockStart)
	}

	remove := func(p string, all bool) {
		var err error
		if all {
			err = os.RemoveAll(p)
		} else {
			err = os.Remove(p)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			warn("couldn't remove %s: %v", p, err)
		}
	}
	remove(e.Home.VersionsDir(), true)
	remove(e.Home.DefaultFile(), false)
	remove(e.envFile(), false)
	// Files a failed update or install.sh left behind.
	staged, _ := filepath.Glob(filepath.Join(e.Home.BinDir(), ".g*-new-*"))
	scripted, _ := filepath.Glob(filepath.Join(e.Home.BinDir(), ".gander-install-*"))
	for _, p := range append(staged, scripted...) {
		remove(p, false)
	}
	// The binary goes last, and only if all else went, so a failed removal can be run
	// again. On Unix a running program can delete its own file.
	if problems > 0 {
		return errors.New("gander isn't fully removed; fix the problems above and run \"gander self uninstall\" again")
	}
	if sameFile(e.goose(), e.gander()) {
		remove(e.goose(), false)
	}
	remove(e.gander(), false)
	if problems > 0 {
		return errors.New("gander isn't fully removed; delete what's left by hand")
	}

	for _, d := range []string{e.Home.BinDir(), e.Home.Dir} {
		err := os.Remove(d)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			continue
		}
		var names []string
		left, _ := os.ReadDir(d)
		for _, de := range left {
			names = append(names, de.Name())
		}
		if len(names) == 0 {
			fmt.Fprintf(e.Stderr, "gander: warning: couldn't remove %s: %v\n", d, err)
		} else {
			fmt.Fprintf(e.Stderr, "Left %s in place, since it holds files gander didn't make: %s\n", d, strings.Join(names, ", "))
		}
		break
	}
	fmt.Fprintf(e.Stderr, "Removed gander and its Goose versions from %s\n", e.Home.Dir)
	return nil
}
