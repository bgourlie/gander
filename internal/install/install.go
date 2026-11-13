// Package install installs Goose versions into gander's home.
//
// An install takes the version's lock, downloads and verifies the archive, unpacks it
// into a staging folder inside versions/, checks that its compiler runs and reports the
// right version, writes the .gander-installed marker, and renames the folder into place.
// A folder without the marker is never treated as installed.
package install

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bgourlie/gander/internal/fsutil"
	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/lock"
	"github.com/bgourlie/gander/internal/platform"
	"github.com/bgourlie/gander/internal/release"
	"github.com/bgourlie/gander/internal/version"
)

// Installer installs versions. Every field but CheckSystem must be set.
type Installer struct {
	Home   home.Home
	Client *release.Client
	Target platform.Target // the build to install

	// CheckSystem, if set, runs before anything is downloaded, to refuse a machine below
	// Goose's OS minimums.
	CheckSystem func() error

	Stderr   io.Writer // messages and progress
	Progress bool      // draw download progress; only when Stderr is a terminal
}

// lockTimeout is how long to wait for another process installing or removing the same
// version. Ctrl-C stops the wait sooner.
const lockTimeout = 5 * time.Minute

// Install installs v unless it's already installed, and reports whether it did.
func (in *Installer) Install(ctx context.Context, v version.Version) (bool, error) {
	if in.Home.IsInstalled(v) {
		return false, nil
	}
	if in.CheckSystem != nil {
		if err := in.CheckSystem(); err != nil {
			return false, err
		}
	}
	versions := in.Home.VersionsDir()
	if err := os.MkdirAll(versions, 0o755); err != nil {
		return false, fmt.Errorf("can't create %s: %w", versions, err)
	}

	l, err := lockVersion(ctx, in.Home, v, in.Stderr)
	if err != nil {
		return false, err
	}
	defer l.Release()

	// Whoever held the lock may have just installed it (proto#1063).
	if in.Home.IsInstalled(v) {
		return false, nil
	}
	sweep(in.Home, v)
	if err := in.install(ctx, v); err != nil {
		return false, err
	}
	return true, nil
}

// StagePattern is the os.MkdirTemp pattern for v's staging folders. "_" can't appear in
// a tag, so v2026.4's pattern never matches v2026.4-rc1's folders.
func StagePattern(v version.Version) string { return home.TmpPrefix + v.String() + "_*" }

// lockVersion takes v's lock, which both installing and uninstalling v hold, and says so
// on stderr when it has to wait.
func lockVersion(ctx context.Context, h home.Home, v version.Version, stderr io.Writer) (*lock.Lock, error) {
	return lock.Acquire(ctx, h.LockFile(v), lock.Options{
		Timeout: lockTimeout,
		Waiting: func() { fmt.Fprintf(stderr, "gander: waiting for another install of %s to finish\n", v) },
		Warn:    func(msg string) { fmt.Fprintf(stderr, "gander: warning: %s\n", msg) },
	})
}

// sweep removes v's leftover staging folders. It runs only while holding v's lock, so it
// never removes a folder another install is using (rustup#2246).
func sweep(h home.Home, v version.Version) {
	old, _ := filepath.Glob(filepath.Join(h.VersionsDir(), StagePattern(v)))
	for _, d := range old {
		os.RemoveAll(d)
	}
}

func (in *Installer) install(ctx context.Context, v version.Version) error {
	rel, apiErr := in.Client.Get(ctx, v)
	var rl *release.RateLimitError
	switch {
	case errors.As(apiErr, &rl):
		// SHA256SUMS can still be read without the API.
	case apiErr != nil:
		return apiErr
	}
	name := release.AssetName(v, in.Target)
	if rel != nil {
		if _, ok := rel.Asset(name); !ok {
			builds := rel.Builds(v)
			if len(builds) == 0 {
				return fmt.Errorf("Goose %s has no compiler builds attached; pick another version from \"gander list --available\"", v)
			}
			return fmt.Errorf("Goose %s has no build for %s, only for %s; pick another version from \"gander list --available\"", v, in.Target, strings.Join(builds, ", "))
		}
	}
	want, wantFrom, err := in.Client.Checksum(ctx, v, rel, name, apiErr)
	if err != nil {
		return err
	}

	// Staging inside versions/ keeps the final rename on one filesystem: a rename across
	// filesystems fails with EXDEV (rustup#2949), and /tmp may be mounted noexec.
	stage, err := os.MkdirTemp(in.Home.VersionsDir(), strings.TrimSuffix(StagePattern(v), "*"))
	if err != nil {
		return fmt.Errorf("can't create a staging folder in %s: %w", in.Home.VersionsDir(), err)
	}
	defer fsutil.RemoveAll(stage)

	archive := filepath.Join(stage, name)
	if err := in.download(ctx, v, name, archive, want, wantFrom); err != nil {
		return err
	}

	tree := filepath.Join(stage, "tree")
	if err := os.Mkdir(tree, 0o755); err != nil {
		return err
	}
	if err := extract(archive, tree, strings.TrimSuffix(name, in.Target.Archive())); err != nil {
		return fmt.Errorf("%w; report this at https://github.com/aardappel/goose/issues", err)
	}
	os.Remove(archive)

	compiler := filepath.Join(tree, home.ExeName("goose"))
	if fi, err := os.Stat(compiler); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("%s has no %s at its top; report this at https://github.com/aardappel/goose/issues", name, home.ExeName("goose"))
	}
	if err := checkVersion(ctx, compiler, v); err != nil {
		return err
	}

	marker := fmt.Sprintf("sha256:%s %s\n", hex.EncodeToString(want), name)
	if err := os.WriteFile(filepath.Join(tree, home.Marker), []byte(marker), 0o644); err != nil {
		return err
	}
	dest := in.Home.VersionDir(v)
	// A folder without the marker is an unfinished install from some earlier failure;
	// we hold the lock, so nobody else is using it.
	if err := fsutil.RemoveAll(dest); err != nil {
		return fmt.Errorf("can't remove the unfinished install at %s: %w", dest, err)
	}
	if err := fsutil.Rename(tree, dest); err != nil {
		return fmt.Errorf("can't move %s into place at %s: %w", v, dest, err)
	}
	return nil
}

func (in *Installer) download(ctx context.Context, v version.Version, name, dest string, want []byte, wantFrom string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	url := in.Client.DownloadURL(v, name)
	var p release.Progress
	var bar *progressBar
	if in.Progress {
		bar = &progressBar{w: in.Stderr, name: name}
		p = bar.update
	}
	got, n, err := in.Client.Download(ctx, url, f, p)
	if bar != nil {
		bar.done()
	}
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		os.Remove(dest)
		return fmt.Errorf("the download of %s doesn't match its checksum, so it was deleted:\n  expected sha256 %x (from %s)\n  got      sha256 %x over %d bytes\ntry again; if it fails again, report it at https://github.com/aardappel/goose/issues",
			url, want, wantFrom, got, n)
	}
	return nil
}

// checkVersion runs "<compiler> version" and checks that it names v. v2026.4 prints
// "goose v2026.4 (runtime dce889723a23)"; earlier releases print "goose runtime
// 4d3da949e563" with no version, so for those it only checks that the compiler runs.
func checkVersion(ctx context.Context, compiler string, v version.Version) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, compiler, "version")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		err = platform.ExplainExecError(compiler, err)
		if s := strings.TrimSpace(errOut.String()); s != "" {
			err = fmt.Errorf("%w: %s", err, s)
		}
		return fmt.Errorf("the downloaded compiler doesn't run (%s version): %w", compiler, err)
	}
	f := strings.Fields(out.String())
	wrong := len(f) == 0 || f[0] != "goose"
	for _, s := range f[min(1, len(f)):] {
		if reported, err := version.Parse(s); err == nil && strings.HasPrefix(s, "v") && reported != v {
			wrong = true
		}
	}
	if wrong {
		return fmt.Errorf("the compiler in release %s reports %q, not %s; report this at https://github.com/aardappel/goose/issues", v, strings.TrimSpace(out.String()), v)
	}
	return nil
}

// progressBar draws one line of download progress, redrawn in place.
type progressBar struct {
	w     io.Writer
	name  string
	last  time.Time
	drawn bool
}

func (b *progressBar) update(done, total int64) {
	if time.Since(b.last) < 100*time.Millisecond && done != total {
		return
	}
	b.last = time.Now()
	if total > 0 {
		fmt.Fprintf(b.w, "\r  %s  %5.1f / %.1f MB ", b.name, mb(done), mb(total))
	} else {
		fmt.Fprintf(b.w, "\r  %s  %5.1f MB ", b.name, mb(done))
	}
	b.drawn = true
}

func (b *progressBar) done() {
	if b.drawn {
		fmt.Fprintf(b.w, "\r%s\r", strings.Repeat(" ", len(b.name)+28))
	}
}

func mb(n int64) float64 { return float64(n) / 1e6 }
