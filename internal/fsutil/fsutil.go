// Package fsutil holds the file operations gander shares.
package fsutil

import (
	"os"
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
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}
