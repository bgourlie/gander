package self

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bgourlie/gander/internal/fsutil"
)

// Install makes the running gander the installed one: it copies the binary into
// <home>/bin with goose beside it, writes the env script and, with modifyPath, adds the
// block that sources it to the shell's startup files. Running it again over an install
// updates the binary and repairs the rest; installed versions and the default stay.
func (e *Env) Install(modifyPath bool) error {
	// Through sudo, HOME may still be the user's, so root would own files there.
	if os.Geteuid() == 0 && e.Getenv("SUDO_USER") != "" {
		return errors.New("don't install gander with sudo: it installs into your own home folder, so run it as yourself")
	}
	if modifyPath && !filepath.IsAbs(e.Getenv("HOME")) {
		return errors.New("HOME isn't set to an absolute path, so gander can't find your shell's startup files; set HOME, or pass --no-modify-path")
	}
	bin := e.Home.BinDir()
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return fmt.Errorf("can't create %s: %w; set GANDER_HOME to a writable absolute folder", bin, err)
	}
	if sameFile(e.Exe, e.gander()) {
		if err := e.relink(); err != nil {
			return err
		}
	} else {
		staged, err := e.stageCopy(e.Exe)
		if err != nil {
			return err
		}
		if err := e.commit(staged); err != nil {
			os.Remove(staged)
			return err
		}
	}
	if err := fsutil.WriteFile(e.envFile(), []byte(envScript(bin)), 0o644); err != nil {
		return fmt.Errorf("can't write %s: %w", e.envFile(), err)
	}
	fmt.Fprintf(e.Stderr, "Installed gander in %s, with goose beside it\n", bin)

	if !modifyPath {
		fmt.Fprintf(e.Stderr, "Left PATH alone; to put gander on it, add these lines to your shell's startup file:\n%s", indent(e.block()))
	} else {
		e.addToPath()
	}
	e.noteOthers()
	return nil
}

// addToPath adds the block to the startup files, and gander's folder to GitHub Actions'
// PATH. A file it can't write is a warning, never a failed install: rustup aborts on one
// (rustup#2040), and home-manager's files are read-only.
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
	// Each step of a GitHub Actions job is a new shell that reads no startup files.
	if gp := e.Getenv("GITHUB_PATH"); gp != "" {
		f, err := os.OpenFile(gp, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
		if err == nil {
			_, err = fmt.Fprintln(f, e.Home.BinDir())
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
		if err != nil {
			fmt.Fprintf(e.Stderr, "gander: warning: couldn't add gander to GITHUB_PATH (%v)\n", err)
		}
	}
}

// noteOthers names other gander and goose programs on PATH. In new shells gander's
// folder comes first, which hides them; Homebrew has two unrelated tools called goose.
func (e *Env) noteOthers() {
	for _, name := range []string{"gander", "goose"} {
		for _, p := range e.onPath(name) {
			if !sameFile(p, filepath.Join(e.Home.BinDir(), name)) {
				fmt.Fprintf(e.Stderr, "Note: %s is another %s on PATH; in new shells, gander's comes first\n", p, name)
			}
		}
	}
}

// NextSteps says how to use gander in the shell that ran the installer.
func (e *Env) NextSteps() string {
	if filepath.Base(e.Getenv("SHELL")) == "fish" {
		return fmt.Sprintf("To use gander in this shell, run:\n    source %s\n", e.tilde(e.fishFile()))
	}
	return fmt.Sprintf("To use gander in this shell, run:\n    . %s\n", e.envRef())
}

// envRefs are ways a startup file may already name the env script.
func (e *Env) envRefs() []string {
	refs := []string{e.envFile()}
	if r := strings.Trim(e.envRef(), `"'`); r != e.envFile() {
		refs = append(refs, r)
	}
	return refs
}

// tilde shortens a path under HOME for messages.
func (e *Env) tilde(p string) string {
	if h := e.Getenv("HOME"); filepath.IsAbs(h) {
		if rel, err := filepath.Rel(h, p); err == nil && filepath.IsLocal(rel) {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return p
}

func indent(s string) string {
	var b strings.Builder
	for line := range strings.Lines(s) {
		b.WriteString("    " + line)
	}
	return b.String()
}
