//go:build !darwin && !linux && !windows

package platform

import "runtime"

func detect() (Target, error) {
	return Target{}, &NoBuildError{Platform: runtime.GOOS + "-" + runtime.GOARCH}
}

func check(Target) error { return nil }

func explainExecError(_ string, err error) error { return err }
