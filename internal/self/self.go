// Package self installs, updates and removes gander itself: the gander binary and its
// goose link in <home>/bin, the env script that puts that folder first on PATH, and the
// blocks in the user's shell startup files that source it.
package self

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bgourlie/gander/internal/home"
)

// Env is what the self commands work with.
type Env struct {
	Home home.Home
	// Exe is the gander binary that's running.
	Exe string
	// Getenv reads HOME, SHELL, PATH, ZDOTDIR, XDG_CONFIG_HOME, GITHUB_PATH and SUDO_USER.
	Getenv func(string) string
	Stderr io.Writer
}

func (e *Env) gander() string { return filepath.Join(e.Home.BinDir(), home.ExeName("gander")) }
func (e *Env) goose() string  { return filepath.Join(e.Home.BinDir(), home.ExeName("goose")) }

// owned fails unless the running gander is the one in gander's home. Changing a copy
// that a package manager owns corrupts its records (mise#14962, deno#37099), and a copy
// in ~/go/bin belongs to "go install"; this one check covers them all, with no receipt.
// verb is what the caller would do, such as "update".
func (e *Env) owned(verb string) error {
	if sameFile(e.Exe, e.gander()) {
		return nil
	}
	return fmt.Errorf("this gander is %s, not the one gander manages at %s; %s it the way you installed it, or run \"%s self install\" to set up %s",
		e.Exe, e.gander(), verb, e.Exe, e.Home.BinDir())
}

// commit makes staged, a new gander binary inside <home>/bin, the installed gander, and
// points goose at it. goose moves first: renaming over gander alone would leave goose
// linked to the old binary, which is how juliaup lost its julia link (juliaup#1513).
// Each step is a rename, so a failure leaves a working gander and goose.
func (e *Env) commit(staged string) error {
	if err := e.replaceGoose(staged); err != nil {
		return err
	}
	if err := replace(staged, e.gander()); err != nil {
		return fmt.Errorf("can't move the new gander into place at %s: %w", e.gander(), err)
	}
	return nil
}

// relink points goose at the installed gander if it doesn't already.
func (e *Env) relink() error {
	if sameFile(e.gander(), e.goose()) {
		return nil
	}
	return e.replaceGoose(e.gander())
}

// replaceGoose makes goose a hardlink to target, or a copy where a hardlink can't be
// made, swapped in by a rename.
func (e *Env) replaceGoose(target string) error {
	tmp, err := tempPath(e.Home.BinDir(), home.ExeName(".goose-new-*"))
	if err != nil {
		return err
	}
	if err := os.Link(target, tmp); err != nil {
		if err := copyFile(target, tmp); err != nil {
			return fmt.Errorf("can't create %s: %w", e.goose(), err)
		}
	}
	if err := replace(tmp, e.goose()); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("can't create %s: %w", e.goose(), err)
	}
	return nil
}

// stageCopy copies src to a new executable file in <home>/bin and returns its path.
func (e *Env) stageCopy(src string) (string, error) {
	tmp, err := tempPath(e.Home.BinDir(), home.ExeName(".gander-new-*"))
	if err != nil {
		return "", err
	}
	if err := copyFile(src, tmp); err != nil {
		return "", fmt.Errorf("can't copy %s into %s: %w", src, e.Home.BinDir(), err)
	}
	return tmp, nil
}

// removeOld deletes the binaries that replace moved aside on Windows, once nothing runs
// them.
func (e *Env) removeOld() {
	old, _ := filepath.Glob(filepath.Join(e.Home.BinDir(), ".g*-old-*"))
	for _, p := range old {
		os.Remove(p)
	}
}

// tempPath returns an unused path in dir that matches pattern, as os.CreateTemp reads it.
// A program gets ".exe" on Windows, where tryRun runs it.
func tempPath(dir, pattern string) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("can't write in %s: %w", dir, err)
	}
	f.Close()
	os.Remove(f.Name())
	return f.Name(), nil
}

// copyFile writes src's bytes to dst, a new executable file. The mode is set
// explicitly: OpenFile's mode passes through the umask (go-update#35).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Chmod(0o755)
	}
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
	}
	return err
}

func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	return err == nil && os.SameFile(fa, fb)
}
