// Package fsutil holds the file operations gander shares.
package fsutil

import (
	"os"
	"os/exec"
	"path/filepath"
)

// WriteFile writes data to a new file beside path and renames it over path, so readers
// see the old file or the new one, never half of it. macOS also kills a signed binary
// that was modified in place, so gander never changes a file in place.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Chmod(perm); err != nil {
		return err
	}
	// Without a sync, a crash soon after the rename can leave the new name holding an
	// empty file.
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}

// Rename renames old to new, retrying on Windows while antivirus holds either one.
func Rename(old, new string) error {
	return retry(func() error { return os.Rename(old, new) })
}

// RemoveAll removes path and everything in it, retrying on Windows while antivirus holds
// a file.
func RemoveAll(path string) error {
	return retry(func() error { return os.RemoveAll(path) })
}

// OnPath lists the programs called name in the folders of path, a PATH value, in the
// order a shell searches them. On Windows each of PATHEXT's extensions is tried, so
// "goose" finds goose.exe and goose.cmd. Relative folders are skipped, since what they
// hold depends on the current folder.
func OnPath(path, name string) []string {
	var found []string
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if p, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			found = append(found, p)
		}
	}
	return found
}
