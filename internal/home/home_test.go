package home

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bgourlie/gander/internal/version"
)

func TestInstalledNeedsTheMarker(t *testing.T) {
	h := Home{Dir: t.TempDir()}
	if vs, err := h.Installed(); err != nil || vs != nil {
		t.Errorf("with no versions folder: %v, %v", vs, err)
	}
	for _, name := range []string{"v1.0", "v1.10", "v1.9", "v2.0-rc1", "v3.0", ".tmp-v4.0_1", "2.5", "junk"} {
		d := filepath.Join(h.VersionsDir(), name)
		os.MkdirAll(d, 0o755)
		if name != "v3.0" { // v3.0 never finished installing
			os.WriteFile(filepath.Join(d, Marker), nil, 0o644)
		}
	}
	os.WriteFile(filepath.Join(h.VersionsDir(), "v1.0.lock"), nil, 0o644)
	vs, err := h.Installed()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range vs {
		got = append(got, v.String())
	}
	if want := "v2.0-rc1 v1.10 v1.9 v1.0"; strings.Join(got, " ") != want {
		t.Errorf("Installed() = %v, want %s", got, want)
	}
	if h.IsInstalled(version.MustParse("v3.0")) {
		t.Error("v3.0 has no marker")
	}
}
