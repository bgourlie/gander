package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/version"
)

// Uninstall removes version v from h. It holds v's lock, so it never removes a version
// that is still installing.
func Uninstall(ctx context.Context, h home.Home, v version.Version, stderr io.Writer) error {
	l, err := lockVersion(ctx, h, v, stderr)
	if err != nil {
		return err
	}
	defer l.Release()
	sweep(h, v)

	// Rename the folder aside first: its marker goes with it in one step, so a removal
	// that fails halfway never leaves something that looks installed.
	dest := h.VersionDir(v)
	stage, err := os.MkdirTemp(h.VersionsDir(), strings.TrimSuffix(StagePattern(v), "*"))
	if err != nil {
		return fmt.Errorf("can't create a staging folder in %s: %w", h.VersionsDir(), err)
	}
	if err := os.Rename(dest, filepath.Join(stage, "tree")); err != nil {
		os.Remove(stage)
		return fmt.Errorf("can't remove %s: %w", dest, err)
	}
	if err := os.RemoveAll(stage); err != nil {
		return fmt.Errorf("removed %s, but couldn't delete all of %s (%w); delete it by hand", v, stage, err)
	}
	return nil
}
