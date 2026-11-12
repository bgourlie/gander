// Package resolve decides which Goose version is in use. The proxy, "gander current",
// "gander dir" and the other commands all call Resolve, so they never disagree.
package resolve

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/version"
	"github.com/bgourlie/gander/internal/versionfile"
)

// Source says where a version came from, highest priority first.
type Source int

const (
	Plus    Source = iota // a leading +version argument
	Env                   // GANDER_GOOSE
	Pin                   // the nearest .goose-version
	Default               // <home>/default
)

// Result is the version in use, why, and where its compiler is.
type Result struct {
	Version   version.Version
	Source    Source
	File      string // the .goose-version or default file, for Pin and Default
	Path      string // the compiler's absolute path
	Installed bool
}

// Reason says why this version was chosen, for messages and "gander current".
func (r Result) Reason() string {
	switch r.Source {
	case Plus:
		return "from +" + r.Version.String() + " on the command line"
	case Env:
		return "from GANDER_GOOSE"
	case Pin:
		return "pinned by " + r.File
	default:
		return "the default, set in " + r.File
	}
}

// Named reports whether the version was asked for by name, through +version,
// GANDER_GOOSE or a pin. Only those are installed automatically: a missing default is
// an error, never a reinstall.
func (r Result) Named() bool { return r.Source != Default }

// Request is what the resolver looks at besides gander's home.
type Request struct {
	Plus   string              // the version after a leading "+", or ""
	Getenv func(string) string // reads GANDER_GOOSE
	Dir    string              // where to start looking for .goose-version; see WorkingDir
	// DirErr, if set, is why Dir is unknown. It's an error only when no +version or
	// GANDER_GOOSE decides the version first.
	DirErr error
}

// NoVersionError means nothing selects a version: no +version, no GANDER_GOOSE, no
// .goose-version and no default.
type NoVersionError struct{ Dir string }

func (e *NoVersionError) Error() string {
	return fmt.Sprintf("no Goose version is selected: there's no %s in %s or above it, GANDER_GOOSE isn't set, and no default is set; run \"gander install\" to install the latest version and make it the default", versionfile.Name, e.Dir)
}

// Resolve returns the version in use.
func Resolve(h home.Home, req Request) (Result, error) {
	r, err := choose(h, req)
	if err != nil {
		return Result{}, err
	}
	r.Path = h.Compiler(r.Version)
	r.Installed = h.IsInstalled(r.Version)
	return r, nil
}

func choose(h home.Home, req Request) (Result, error) {
	if req.Plus != "" {
		v, err := version.Parse(req.Plus)
		if err != nil {
			return Result{}, fmt.Errorf("+%s: %w", req.Plus, err)
		}
		return Result{Version: v, Source: Plus}, nil
	}
	if s := req.Getenv("GANDER_GOOSE"); s != "" {
		v, err := version.Parse(s)
		if err != nil {
			return Result{}, fmt.Errorf("GANDER_GOOSE: %w; set it to a version or unset it", err)
		}
		return Result{Version: v, Source: Env}, nil
	}
	if req.DirErr != nil {
		return Result{}, req.DirErr
	}
	path, v, ok, err := FindPin(req.Dir)
	if err != nil {
		return Result{}, err
	}
	if ok {
		return Result{Version: v, Source: Pin, File: path}, nil
	}
	v, ok, err = h.Default()
	if err != nil {
		return Result{}, err
	}
	if ok {
		return Result{Version: v, Source: Default, File: h.DefaultFile()}, nil
	}
	return Result{}, &NoVersionError{Dir: req.Dir}
}

// FindPin looks for the nearest .goose-version in dir and each folder above it. A file
// that exists but can't be read or parsed is an error, never a reason to look further.
func FindPin(dir string) (path string, v version.Version, ok bool, err error) {
	for d := dir; ; d = filepath.Dir(d) {
		p := filepath.Join(d, versionfile.Name)
		v, err := versionfile.Read(p)
		if err == nil {
			return p, v, true, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", version.Version{}, false, err
		}
		if filepath.Dir(d) == d {
			return "", version.Version{}, false, nil
		}
	}
}

// WorkingDir returns the physical current directory. os.Getwd returns $PWD when it names
// the current directory, so a terminal (logical path) and an editor (physical path) could
// otherwise find different pins under a symlink.
func WorkingDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("can't find the current directory: %w", err)
	}
	if p, err := filepath.EvalSymlinks(wd); err == nil {
		wd = p
	}
	return wd, nil
}
