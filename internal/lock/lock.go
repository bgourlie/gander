// Package lock holds an exclusive lock on a file, such as versions/<tag>.lock while that
// version installs, so an editor's language server and a terminal starting at the same
// moment don't both unpack the same version (rustup#988, proto#1063).
//
// The lock file is never deleted: deleting a held lock file lets a third process lock a
// new file of the same name. Stale locks can't happen, since the kernel releases a lock
// when its process dies.
package lock

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Options says how long to wait and whom to tell.
type Options struct {
	// Timeout is how long to wait for another process to release the lock.
	Timeout time.Duration
	// Waiting, if set, is called once when another process has held the lock for a
	// moment.
	Waiting func()
	// Warn, if set, is called when this filesystem can't lock files and gander carries
	// on without the lock.
	Warn func(msg string)
}

// Lock is a held lock.
type Lock struct {
	f *os.File // nil when the filesystem can't lock
}

// TimeoutError means another process held the lock for longer than Options.Timeout.
type TimeoutError struct {
	Path    string
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("gave up after %s waiting for another gander to release %s; wait for it to finish, then try again", e.Timeout, e.Path)
}

// Acquire takes the lock on path, creating the file if needed, and waits while another
// process holds it.
func Acquire(ctx context.Context, path string, opt Options) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("can't open the lock file %s: %w", path, err)
	}
	start := time.Now()
	told := false
	for {
		held, err := tryLock(f)
		if err != nil {
			f.Close()
			if !unsupported(err) {
				return nil, fmt.Errorf("can't lock %s: %w", path, err)
			}
			// rustup 1.29 hard-failed on Termux (rustup#5151) and juliaup panicked on a
			// Cray home directory (juliaup#252). The marker file keeps installs correct
			// without the lock, which only prevents duplicate downloads.
			if opt.Warn != nil {
				opt.Warn(fmt.Sprintf("this filesystem can't lock %s (%v); carrying on without the lock", path, err))
			}
			return &Lock{}, nil
		}
		if held {
			return &Lock{f: f}, nil
		}
		// Say nothing about a short wait.
		if !told && opt.Waiting != nil && time.Since(start) >= 150*time.Millisecond {
			opt.Waiting()
			told = true
		}
		if time.Since(start) >= opt.Timeout {
			f.Close()
			return nil, &TimeoutError{Path: path, Timeout: opt.Timeout}
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Release releases the lock. The file stays.
func (l *Lock) Release() error {
	if l.f == nil {
		return nil
	}
	err := unlock(l.f)
	if cerr := l.f.Close(); err == nil {
		err = cerr
	}
	l.f = nil
	return err
}
