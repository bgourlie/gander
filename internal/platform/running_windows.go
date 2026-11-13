package platform

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// Windows refuses to open a running program's file for writing, with a sharing
// violation; any other answer means it isn't running.
func running(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		f.Close()
		return false
	}
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
