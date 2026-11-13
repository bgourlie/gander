//go:build !windows

package platform

func running(string) bool { return false }
