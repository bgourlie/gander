package platform

import (
	"runtime"
	"testing"
)

func TestDetectMac(t *testing.T) {
	tg, err := Detect()
	if err != nil {
		t.Fatal(err)
	}
	// An arm64 gander runs only on Apple silicon; an amd64 one may run under Rosetta,
	// where the answer must still be arm64 on Apple silicon.
	if runtime.GOARCH == "arm64" && tg.String() != "macos-arm64" {
		t.Errorf("Detect() = %s on arm64", tg)
	}
	if tg.OS != "macos" {
		t.Errorf("Detect() = %s", tg)
	}
	if err := Check(tg); err != nil {
		t.Errorf("Check: %v", err)
	}
}
