//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package term

import "golang.org/x/sys/unix"

func isTerminal(fd uintptr) bool {
	_, err := unix.IoctlGetTermios(int(fd), ioctlGetTermios)
	return err == nil
}
