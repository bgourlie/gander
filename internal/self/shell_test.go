package self

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The env script puts gander's folder first on PATH exactly once, in every shell that
// sources it, whatever the folder's name.
func TestEnvScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no sh")
	}
	bin := filepath.Join(t.TempDir(), "my bin*")
	env := filepath.Join(t.TempDir(), "env")
	os.WriteFile(env, []byte(envScript(bin)), 0o644)
	ran := 0
	for _, sh := range []string{"/bin/sh", "/bin/dash", "/bin/bash", "/bin/zsh"} {
		if _, err := os.Stat(sh); err != nil {
			continue
		}
		ran++
		for path, want := range map[string]string{
			"/a:" + bin + ":/b:" + bin: bin + ":/a:/b",
			"/a:/b":                    bin + ":/a:/b",
			bin:                        bin,
		} {
			cmd := exec.Command(sh, "-c", `. "$1"; printf '%s' "$PATH"`, "sh", env)
			// An empty HOME, so zsh doesn't read this machine's .zshenv.
			empty := t.TempDir()
			cmd.Env = []string{"PATH=" + path, "HOME=" + empty, "ZDOTDIR=" + empty}
			out, err := cmd.Output()
			if err != nil || string(out) != want {
				t.Errorf("%s with PATH=%s: %q, %v; want %q", sh, path, out, err, want)
			}
		}
	}
	if ran == 0 {
		t.Skip("no shells found")
	}
}

func TestStartupBlocks(t *testing.T) {
	dir := t.TempDir()
	block := blockStart + "\nif [ -f x ]; then . x; fi\n" + blockEnd + "\n"
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }

	// Added after the user's last line, even one with no newline, and removed back to it.
	rc := filepath.Join(dir, ".bashrc")
	os.WriteFile(rc, []byte("alias ll='ls -l'"), 0o600)
	if did, err := addBlock(rc, block, "/h/.gander/env"); !did || err != nil {
		t.Fatalf("addBlock = %v, %v", did, err)
	}
	if got := read(rc); got != "alias ll='ls -l'\n\n"+block {
		t.Errorf("after addBlock:\n%s", got)
	}
	if did, _ := addBlock(rc, block); did || strings.Count(read(rc), blockStart) != 1 {
		t.Error("a second addBlock added another block")
	}
	if did, err := removeBlock(rc); !did || err != nil || read(rc) != "alias ll='ls -l'\n" {
		t.Errorf("removeBlock = %v, %v; left %q", did, err, read(rc))
	}
	if fi, _ := os.Stat(rc); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode is now %v", fi.Mode().Perm())
	}

	// A startup file that's a link into a dotfiles folder stays a link.
	target := filepath.Join(dir, "dotfiles", "zshrc")
	os.MkdirAll(filepath.Dir(target), 0o755)
	os.WriteFile(target, []byte("export EDITOR=vi\n"), 0o644)
	link := filepath.Join(dir, ".zshrc")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	addBlock(link, block)
	removeBlock(link)
	addBlock(link, block)
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the link was replaced")
	}
	if !strings.Contains(read(target), blockStart) {
		t.Error("the block didn't reach the link's target")
	}

	// The user's own line sourcing the env script counts; a commented one doesn't.
	own := filepath.Join(dir, ".profile")
	os.WriteFile(own, []byte(". /h/.gander/env\n"), 0o644)
	if did, _ := addBlock(own, block, "/h/.gander/env"); did {
		t.Error("added a block beside the user's own line")
	}
	os.WriteFile(own, []byte("# . /h/.gander/env\n"), 0o644)
	if did, _ := addBlock(own, block, "/h/.gander/env"); !did {
		t.Error("a commented-out line stopped the block")
	}

	// A file with markers out of order or doubled is left alone.
	odd := blockEnd + "\nkeep me\n" + blockStart + "\n"
	os.WriteFile(own, []byte(odd), 0o644)
	if did, err := removeBlock(own); did || err == nil || read(own) != odd {
		t.Errorf("removeBlock on misordered markers = %v, %v", did, err)
	}
}
