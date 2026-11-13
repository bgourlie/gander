package self

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bgourlie/gander/internal/fsutil"
	"github.com/bgourlie/gander/internal/home"
)

// Install makes the running gander the installed one: it copies the binary into
// <home>/bin with goose beside it and, with modifyPath, puts that folder on PATH: on
// Unix through an env script that the shell's startup files source, on Windows in the
// user's PATH in the registry. Running it again over an install updates the binary and
// repairs the rest; installed versions and the default stay.
func (e *Env) Install(modifyPath bool) error {
	// Through sudo, HOME may still be the user's, so root would own files there.
	if os.Geteuid() == 0 && e.Getenv("SUDO_USER") != "" {
		return errors.New("don't install gander with sudo: it installs into your own home folder, so run it as yourself")
	}
	if modifyPath {
		if err := e.canModifyPath(); err != nil {
			return err
		}
	}
	bin := e.Home.BinDir()
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return fmt.Errorf("can't create %s: %w; set GANDER_HOME to a writable absolute folder", bin, err)
	}
	e.removeOld()
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
	if err := e.writeEnv(); err != nil {
		return err
	}
	fmt.Fprintf(e.Stderr, "Installed gander in %s, with goose beside it\n", bin)

	if !modifyPath {
		fmt.Fprint(e.Stderr, e.pathByHand())
	} else {
		e.addToPath()
		e.addToGitHubPath()
	}
	e.noteOthers()
	return nil
}

// addToGitHubPath adds gander's folder to GitHub Actions' PATH: each step of a job is a
// new shell that reads no startup files, and on Windows doesn't see the registry change.
func (e *Env) addToGitHubPath() {
	gp := e.Getenv("GITHUB_PATH")
	if gp == "" {
		return
	}
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

// noteOthers names other gander and goose programs on PATH. In new shells gander's
// folder comes first, which hides them; Homebrew has two unrelated tools called goose.
func (e *Env) noteOthers() {
	for _, name := range []string{"gander", "goose"} {
		for _, p := range fsutil.OnPath(e.Getenv("PATH"), name) {
			if !sameFile(p, filepath.Join(e.Home.BinDir(), home.ExeName(name))) {
				fmt.Fprintf(e.Stderr, "Note: %s is another %s on PATH; %s\n", p, name, othersFirst)
			}
		}
	}
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
