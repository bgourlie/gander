package platform

import (
	"debug/elf"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// minGlibc is the oldest glibc Goose's Linux compiler runs on.
const minGlibc = "2.35"

func detect() (Target, error) {
	if runtime.GOARCH == "amd64" {
		return Target{"linux", "x64"}, nil
	}
	return Target{}, &NoBuildError{Platform: "linux-" + archName(runtime.GOARCH)}
}

func archName(goarch string) string {
	switch goarch {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	}
	return goarch
}

// RHEL 9 and Amazon Linux 2023 ship glibc 2.34, and Alpine ships musl. Either way the
// compiler would download fine and then fail to start with a confusing error, so check
// first. When the C library can't be identified, carry on: the check after downloading
// and the translated exec error still catch it.
func check(Target) error {
	interp, have, err := systemLibc()
	if err != nil {
		return nil
	}
	if strings.Contains(filepath.Base(interp), "musl") {
		return fmt.Errorf("Goose's Linux compiler needs glibc %s or later, and this system uses musl (%s); run Goose on a glibc distribution or in a container, or build it from source: https://github.com/aardappel/goose", minGlibc, interp)
	}
	if have != "" && !atLeast(have, minGlibc) {
		return glibcTooOld(minGlibc, have)
	}
	return nil
}

func glibcTooOld(need, have string) error {
	return fmt.Errorf("Goose's Linux compiler needs glibc %s or later, and this system has glibc %s; use a newer distribution (RHEL 9 and Amazon Linux 2023 have 2.34) or a container, or build Goose from source: https://github.com/aardappel/goose", need, have)
}

// systemLibc finds the C library the way uv does (crates/uv-platform/src/libc.rs): the
// ELF interpreter of /bin/sh says glibc or musl, and libc.so.6's version definitions say
// which glibc. No subprocess is started.
func systemLibc() (interp, glibc string, err error) {
	sh, err := elf.Open("/bin/sh")
	if err != nil {
		return "", "", err
	}
	defer sh.Close()
	interp, err = interpreter(sh)
	if err != nil {
		return "", "", err
	}
	if strings.Contains(filepath.Base(interp), "musl") {
		return interp, "", nil
	}
	dirs := []string{filepath.Dir(interp)}
	if real, err := filepath.EvalSymlinks(interp); err == nil {
		dirs = append([]string{filepath.Dir(real)}, dirs...)
	}
	dirs = append(dirs, "/lib/x86_64-linux-gnu", "/usr/lib/x86_64-linux-gnu", "/lib64", "/usr/lib64", "/lib", "/usr/lib")
	for _, d := range dirs {
		lib, err := elf.Open(filepath.Join(d, "libc.so.6"))
		if err != nil {
			continue
		}
		defs, err := lib.DynamicVersions()
		lib.Close()
		if err != nil {
			continue
		}
		for _, def := range defs {
			glibc = maxGlibc(glibc, def.Name)
		}
		if glibc != "" {
			return interp, glibc, nil
		}
	}
	return interp, "", errors.New("libc.so.6 not found")
}

func interpreter(f *elf.File) (string, error) {
	for _, p := range f.Progs {
		if p.Type != elf.PT_INTERP {
			continue
		}
		b := make([]byte, p.Filesz)
		if _, err := p.ReadAt(b, 0); err != nil {
			return "", err
		}
		return strings.TrimRight(string(b), "\x00"), nil
	}
	return "", errors.New("no ELF interpreter")
}

// maxGlibc returns the newer of cur and the version in a symbol version name such as
// "GLIBC_2.35". Names that aren't GLIBC_ versions, such as GLIBC_PRIVATE, are ignored.
func maxGlibc(cur, name string) string {
	v, ok := strings.CutPrefix(name, "GLIBC_")
	if !ok || v == "" || v[0] < '0' || v[0] > '9' {
		return cur
	}
	if cur == "" || !atLeast(cur, v) {
		return v
	}
	return cur
}

// A missing ELF interpreter makes exec fail with "no such file or directory" although the
// compiler exists, which sends people looking for the wrong file.
func explainExecError(path string, err error) error {
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return err
	}
	msg := fmt.Sprintf("this system's C library can't run %s", path)
	if f, ferr := elf.Open(path); ferr == nil {
		if interp, ierr := interpreter(f); ierr == nil {
			msg += fmt.Sprintf(": it needs the loader %s, which isn't here", interp)
		}
		f.Close()
	}
	if _, nixErr := os.Stat("/etc/NIXOS"); nixErr == nil && os.Getenv("NIX_LD") == "" {
		msg += "; on NixOS, enable nix-ld (programs.nix-ld.enable = true) or run goose through steam-run"
	} else {
		msg += fmt.Sprintf("; Goose's Linux compiler needs glibc %s or later", minGlibc)
	}
	return errors.New(msg)
}
