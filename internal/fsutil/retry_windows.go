package fsutil

import (
	"errors"
	"time"

	"golang.org/x/sys/windows"
)

// retry runs op until it succeeds, fails for another reason, or about 10 seconds pass.
// Antivirus opens a new file to scan it, and until it's done Windows refuses to rename
// or delete it, or the folder it's in (rustup#1870, uv#1491). Go's own toolchain retries
// the same two errors (cmd/internal/robustio); fs.ErrPermission matches only the first.
func retry(op func() error) error {
	start := time.Now()
	delay := 10 * time.Millisecond
	for {
		err := op()
		if err == nil || time.Since(start) > 10*time.Second ||
			!errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return err
		}
		time.Sleep(delay)
		delay = min(2*delay, time.Second)
	}
}
