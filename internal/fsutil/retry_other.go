//go:build !windows

package fsutil

func retry(op func() error) error { return op() }
