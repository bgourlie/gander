package platform

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// minMacOS is the oldest macOS Goose's compiler runs on.
const minMacOS = "13.3"

// Under Rosetta, an x64 gander (or one started from a translated terminal) sees amd64,
// would install the Intel build, and goose would then build x86_64 programs
// (rustup#3063). hw.optional.arm64 reports the hardware; it doesn't exist on Intel Macs.
func detect() (Target, error) {
	arm, err := unix.SysctlUint32("hw.optional.arm64")
	if err == nil && arm == 1 {
		return Target{"macos", "arm64"}, nil
	}
	if err != nil && !errors.Is(err, unix.ENOENT) {
		return Target{}, fmt.Errorf("can't tell whether this Mac has Apple silicon: sysctl hw.optional.arm64: %w", err)
	}
	return Target{"macos", "x64"}, nil
}

func check(Target) error {
	have, err := unix.Sysctl("kern.osproductversion")
	if err != nil {
		return fmt.Errorf("can't read the macOS version: sysctl kern.osproductversion: %w", err)
	}
	if !atLeast(have, minMacOS) {
		return fmt.Errorf("Goose's compiler needs macOS %s or later, and this Mac runs macOS %s; update macOS to use Goose", minMacOS, have)
	}
	return nil
}

func explainExecError(_ string, err error) error { return err }
