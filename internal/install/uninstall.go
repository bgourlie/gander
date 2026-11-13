package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bgourlie/gander/internal/fsutil"
	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/platform"
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
	if err := NotRunning(h, v); err != nil {
		return err
	}
	sweep(h, v)

	// The marker goes first, so a removal that fails halfway leaves debris that the next
	// install clears, never something that looks installed. The folder isn't renamed
	// aside: OverlayFS refuses to rename a directory from a lower image layer (EXDEV,
	// rustup#2949), as in a container built with this version already installed.
	dest := h.VersionDir(v)
	if err := os.Remove(filepath.Join(dest, home.Marker)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("can't remove %s: %w", dest, err)
	}
	if err := fsutil.RemoveAll(dest); err != nil {
		return fmt.Errorf("removed %s, but couldn't delete all of %s (%w); delete it by hand", v, dest, err)
	}
	return nil
}

// NotRunning fails if v's compiler is running, as an editor's language server may be.
// Windows won't delete a running program, so removing v would stop halfway, after
// deleting the library files the running compiler still reads (uv#11930). Elsewhere it
// never fails.
func NotRunning(h home.Home, v version.Version) error {
	if p := h.Compiler(v); platform.Running(p) {
		return fmt.Errorf("Goose %s is running from %s, perhaps as an editor's language server, and Windows can't delete a running program; stop it, then try again", v, p)
	}
	return nil
}
