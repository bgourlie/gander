// Package home knows the layout of gander's home folder:
//
//	<home>/bin/gander, bin/goose   the same binary
//	<home>/env                     sh that puts bin/ on PATH (see package self)
//	<home>/default                 one line: the default version
//	<home>/versions/<tag>/         an unpacked release, complete once .gander-installed exists
//	<home>/versions/<tag>.lock     held while that version installs
package home

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/bgourlie/gander/internal/version"
	"github.com/bgourlie/gander/internal/versionfile"
)

// Marker is the file whose presence makes a version's folder a complete install. It is
// written last, just before the folder is renamed into place.
const Marker = ".gander-installed"

// TmpPrefix starts the names of staging folders in versions/.
const TmpPrefix = ".tmp-"

// Home is gander's home folder.
type Home struct {
	Dir string
}

// FromEnv returns the home named by GANDER_HOME, else the platform's default.
func FromEnv(getenv func(string) string) (Home, error) {
	if d := getenv("GANDER_HOME"); d != "" {
		if !filepath.IsAbs(d) {
			return Home{}, fmt.Errorf("GANDER_HOME is %q, which isn't an absolute path; set it to an absolute path or unset it", d)
		}
		return Home{Dir: filepath.Clean(d)}, nil
	}
	d, err := defaultDir()
	if err != nil {
		return Home{}, fmt.Errorf("can't find gander's home folder (%w); set GANDER_HOME to an absolute path", err)
	}
	return Home{Dir: d}, nil
}

// BinDir is where gander and the goose proxy live.
func (h Home) BinDir() string { return filepath.Join(h.Dir, "bin") }

// VersionsDir holds one folder per installed version.
func (h Home) VersionsDir() string { return filepath.Join(h.Dir, "versions") }

// DefaultFile holds the default version.
func (h Home) DefaultFile() string { return filepath.Join(h.Dir, "default") }

// VersionDir is the folder version v installs into.
func (h Home) VersionDir(v version.Version) string {
	return filepath.Join(h.VersionsDir(), v.String())
}

// Compiler is the path of version v's goose executable.
func (h Home) Compiler(v version.Version) string {
	return filepath.Join(h.VersionDir(v), ExeName("goose"))
}

// LockFile is the file held while version v installs or uninstalls.
func (h Home) LockFile(v version.Version) string {
	return filepath.Join(h.VersionsDir(), v.String()+".lock")
}

// IsInstalled reports whether version v is completely installed.
func (h Home) IsInstalled(v version.Version) bool {
	_, err := os.Stat(filepath.Join(h.VersionDir(v), Marker))
	return err == nil
}

// Installed lists the completely installed versions, newest first. Folders without a
// marker are unfinished installs and are left out.
func (h Home) Installed() ([]version.Version, error) {
	entries, err := os.ReadDir(h.VersionsDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var vs []version.Version
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		v, err := version.Parse(e.Name())
		if err != nil || v.String() != e.Name() || !h.IsInstalled(v) {
			continue
		}
		vs = append(vs, v)
	}
	slices.SortFunc(vs, func(a, b version.Version) int { return version.Compare(b, a) })
	return vs, nil
}

// Default returns the default version. ok is false when none is set.
func (h Home) Default() (v version.Version, ok bool, err error) {
	v, err = versionfile.Read(h.DefaultFile())
	if errors.Is(err, fs.ErrNotExist) {
		return version.Version{}, false, nil
	}
	if err != nil {
		return version.Version{}, false, err
	}
	return v, true, nil
}

// SetDefault makes v the default.
func (h Home) SetDefault(v version.Version) error {
	if err := os.MkdirAll(h.Dir, 0o755); err != nil {
		return err
	}
	return versionfile.Write(h.DefaultFile(), v)
}

// ClearDefault removes the default, so a later "goose" says none is set instead of
// reinstalling it.
func (h Home) ClearDefault() error {
	err := os.Remove(h.DefaultFile())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// ExeName adds ".exe" on Windows.
func ExeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
