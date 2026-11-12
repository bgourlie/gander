//go:build !windows

package home

import (
	"os"
	"path/filepath"
)

func defaultDir() (string, error) {
	d, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, ".gander"), nil
}
