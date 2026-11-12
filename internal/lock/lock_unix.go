//go:build unix

package lock

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func tryLock(f *os.File) (held bool, err error) {
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	return false, err
}

func unlock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}

// unsupported reports the errors a filesystem gives when it can't lock at all. 524 is
// Linux's internal ENOTSUPP, which leaks out of some network filesystems.
func unsupported(err error) bool {
	return errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) ||
		errors.Is(err, unix.ENOLCK) || errors.Is(err, unix.ENOSYS) ||
		errors.Is(err, syscall.Errno(524))
}
