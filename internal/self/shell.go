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

// The lines around gander's block in a startup file. Plain "# pnpm" markers once paired
// with a user's own comment and deleted what lay between (pnpm#7067), so these are
// distinctive, and removal needs exactly one of each.
const (
	blockStart = "# >>> gander >>>"
	blockEnd   = "# <<< gander <<<"
)

// envFile is the script the startup files source. It holds all of gander's PATH logic,
// so "self install" can change it without touching the user's files.
func (e *Env) envFile() string { return filepath.Join(e.Home.Dir, "env") }

// envScript puts bin first on PATH, taking it out of wherever else it was. Moving it each
// time matters on macOS: path_helper and "brew shellenv" run after .zshenv and put their
// folders first, and Homebrew has other programs named goose. It prints nothing and runs
// no programs, since editors source it to learn the environment. It works in sh, dash,
// bash and zsh, which doesn't split unquoted words.
func envScript(bin string) string {
	return `# gander's PATH setup, sourced from your shell's startup files. "gander self install"
# rewrites this file, and "gander self uninstall" removes it.
_gander_bin=` + shQuote(bin) + `
_gander_path=":$PATH:"
while :; do
  case $_gander_path in
    *":$_gander_bin:"*) _gander_path="${_gander_path%%":$_gander_bin:"*}:${_gander_path#*":$_gander_bin:"}" ;;
    *) break ;;
  esac
done
_gander_path=${_gander_path#:}
_gander_path=${_gander_path%:}
PATH="$_gander_bin${_gander_path:+:$_gander_path}"
export PATH
unset _gander_bin _gander_path
`
}

// block is what gander adds to a startup file. The source line is guarded: after an
// "rm -rf ~/.gander", sh and dash stop at a "." of a missing file, and SDDM's Wayland
// session sources .profile with sh (deno_install#328). "if" rather than "&&", which as a
// file's last line would make it return 1.
func (e *Env) block() string {
	p := e.envRef()
	return blockStart + "\n" + "if [ -f " + p + " ]; then . " + p + "; fi\n" + blockEnd + "\n"
}

// envRef is the env script's path as the block writes it: "$HOME/..." when it's under
// HOME, so a startup file shared across machines keeps working (juliaup#799), else
// quoted whole.
func (e *Env) envRef() string {
	env := e.envFile()
	if h := e.Getenv("HOME"); filepath.IsAbs(h) {
		if rel, err := filepath.Rel(h, env); err == nil && filepath.IsLocal(rel) && !strings.ContainsAny(rel, "\"$`\\\n") {
			return `"$HOME/` + filepath.ToSlash(rel) + `"`
		}
	}
	return shQuote(env)
}

// fishFile is gander's file in fish's conf.d. fish_add_path's default, a universal
// variable, outlives an uninstall; --path changes $PATH in each new shell instead.
func (e *Env) fishFile() string {
	cfg := e.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(cfg) {
		cfg = filepath.Join(e.Getenv("HOME"), ".config")
	}
	return filepath.Join(cfg, "fish", "conf.d", "gander.fish")
}

func fishScript(bin string) string {
	q := "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(bin) + "'"
	return "# gander's PATH setup. \"gander self uninstall\" removes this file.\nfish_add_path --path --move " + q + "\n"
}

// startupFiles lists the sh-family files to add the block to. bash follows rustup:
// always .profile; .bash_profile, .bash_login and .bashrc only if they exist, since
// creating one would hide .profile from login shells. zsh's are written only when zsh
// is the login shell or on PATH, so a bash-only machine doesn't get a .zshrc
// (uv#14547). With all, it lists every file gander might have written, for uninstall.
func (e *Env) startupFiles(all bool) []string {
	h := e.Getenv("HOME")
	files := []string{filepath.Join(h, ".profile")}
	for _, name := range []string{".bash_profile", ".bash_login", ".bashrc"} {
		if p := filepath.Join(h, name); all || exists(p) {
			files = append(files, p)
		}
	}
	if all || e.hasShell("zsh") {
		zdot := e.Getenv("ZDOTDIR")
		if !filepath.IsAbs(zdot) {
			zdot = h
		}
		files = append(files, filepath.Join(zdot, ".zshenv"), filepath.Join(zdot, ".zshrc"))
	}
	return files
}

// hasShell reports whether name is the login shell or on PATH.
func (e *Env) hasShell(name string) bool {
	if filepath.Base(e.Getenv("SHELL")) == name {
		return true
	}
	return len(fsutil.OnPath(e.Getenv("PATH"), name)) > 0
}

// addBlock appends block to the startup file at path, creating it if needed, unless the
// file already has gander's start marker or an uncommented line naming the env script,
// as a dotfiles user's own line would. It reports whether it wrote anything.
//
// This is the one place gander changes a file in place. Renaming a new file over a
// startup file would replace a symlink into the user's dotfiles with a plain file
// (ghcup#342), and would succeed even over home-manager's read-only links, breaking
// its next switch. Appending follows the link, and keeps the file's mode and owner.
func addBlock(path, block string, refs ...string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	for line := range strings.Lines(string(data)) {
		t := strings.TrimSpace(line)
		if t == blockStart {
			return false, nil
		}
		if !strings.HasPrefix(t, "#") {
			for _, r := range refs {
				if strings.Contains(t, r) {
					return false, nil
				}
			}
		}
	}
	var add []byte
	if len(data) > 0 {
		// A file without a final newline would run its last line into ours
		// (deno_install#335).
		if data[len(data)-1] != '\n' {
			add = append(add, '\n')
		}
		add = append(add, '\n')
	}
	add = append(add, block...)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return false, err
	}
	_, err = f.Write(add)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err == nil, err
}

// removeBlock takes gander's block out of the startup file at path, with the blank line
// addBlock put before it, and reports whether it did. It removes only exactly one start
// marker followed by exactly one end marker; anything else is left for the user, since
// guessing has deleted users' lines (pnpm#7067, rustup#4529). Like addBlock, it writes
// in place.
func removeBlock(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var lines []string
	var starts, ends []int
	for line := range strings.Lines(string(data)) {
		switch strings.TrimSpace(line) {
		case blockStart:
			starts = append(starts, len(lines))
		case blockEnd:
			ends = append(ends, len(lines))
		}
		lines = append(lines, line)
	}
	if len(starts) == 0 && len(ends) == 0 {
		return false, nil
	}
	if len(starts) != 1 || len(ends) != 1 || ends[0] < starts[0] {
		var at []string
		for _, i := range append(starts, ends...) {
			at = append(at, fmt.Sprint(i+1))
		}
		return false, fmt.Errorf("%s has gander's markers out of order or more than once (lines %s); remove gander's lines from it yourself", path, strings.Join(at, ", "))
	}
	from, to := starts[0], ends[0]+1
	if from > 0 && strings.TrimSpace(lines[from-1]) == "" {
		from--
	}
	out := strings.Join(lines[:from], "") + strings.Join(lines[to:], "")
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(out), fi.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// shQuote quotes s for sh.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
