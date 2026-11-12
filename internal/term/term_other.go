//go:build !(darwin || linux || freebsd || netbsd || openbsd || dragonfly || windows)

package term

func isTerminal(uintptr) bool { return false }
