package self

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bgourlie/gander/internal/fsutil"
	"github.com/bgourlie/gander/internal/install"
)

// Uninstall removes gander: its PATH setup, then only the names gander created in its
// home, the installed versions among them, then the home itself if that leaves it
// empty. Deleting the whole folder would delete whatever else is there: a user who had
// linked ~/.cargo/bin to ~/bin lost ~/bin to rustup (rustup#1072).
// Every step can be repeated, so running it again finishes a removal that failed.
func (e *Env) Uninstall() error {
	if err := e.owned("remove"); err != nil {
		return err
	}
	// Windows can't delete a running compiler, so check them all before changing anything.
	installed, _ := e.Home.Installed()
	for _, v := range installed {
		if err := install.NotRunning(e.Home, v); err != nil {
			return err
		}
	}
	var problems int
	warn := func(format string, args ...any) {
		problems++
		fmt.Fprintf(e.Stderr, "gander: warning: "+format+"\n", args...)
	}
	e.removeFromPath(warn)

	removed := func(p string, err error) {
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			warn("couldn't remove %s: %v", p, err)
		}
	}
	removed(e.Home.VersionsDir(), fsutil.RemoveAll(e.Home.VersionsDir()))
	removed(e.Home.DefaultFile(), os.Remove(e.Home.DefaultFile()))
	removed(e.envFile(), os.Remove(e.envFile()))
	// Files a failed update or install script left behind, and on Windows the binaries
	// an update moved aside.
	var leftovers []string
	for _, pattern := range []string{".g*-new-*", ".g*-old-*", ".gander-install-*"} {
		m, _ := filepath.Glob(filepath.Join(e.Home.BinDir(), pattern))
		leftovers = append(leftovers, m...)
	}
	for _, p := range leftovers {
		removed(p, discard(p))
	}
	// The binary goes last, and only if all else went, so a failed removal can be run
	// again.
	if problems > 0 {
		return errors.New("gander isn't fully removed; fix the problems above and run \"gander self uninstall\" again")
	}
	if sameFile(e.goose(), e.gander()) {
		removed(e.goose(), discard(e.goose()))
	}
	removed(e.gander(), discard(e.gander()))
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
