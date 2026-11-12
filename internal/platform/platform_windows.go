package platform

import (
	"runtime"

	"golang.org/x/sys/windows"
)

// Windows on Arm runs x64 programs only from Windows 11 (build 22000) on.
func detect() (Target, error) {
	switch runtime.GOARCH {
	case "amd64":
		return Target{"windows", "x64"}, nil
	case "arm64":
		if windows.RtlGetVersion().BuildNumber >= 22000 {
			return Target{"windows", "x64"}, nil
		}
	}
	return Target{}, &NoBuildError{Platform: "windows-" + runtime.GOARCH}
}

func check(Target) error { return nil }

func explainExecError(_ string, err error) error { return err }
