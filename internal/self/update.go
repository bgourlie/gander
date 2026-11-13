package self

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bgourlie/gander/internal/platform"
	"github.com/bgourlie/gander/internal/release"
	"github.com/bgourlie/gander/internal/version"
)

// Updater replaces the installed gander with another release.
type Updater struct {
	Env
	Client  *release.Client // gander's releases
	Current string          // the running gander's version
	Target  platform.Target // the build to fetch
}

// Update installs release want, or with a zero want, the latest release if it's newer
// than this one. A named release is installed even if it's older. A build that isn't a
// release, such as one from "go build", counts as older than every release.
func (u *Updater) Update(ctx context.Context, want version.Version) error {
	if err := u.owned("update"); err != nil {
		return err
	}
	cur, curErr := version.Parse(u.Current)
	isRelease := curErr == nil && cur.String() == u.Current
	tag := want
	if tag.IsZero() {
		latest, err := u.Client.LatestTag(ctx)
		if err != nil {
			return err
		}
		tag = latest
		if isRelease && version.Compare(tag, cur) <= 0 {
			fmt.Fprintf(u.Stderr, "gander %s is up to date\n", u.Current)
			return u.relink()
		}
	} else if isRelease && tag == cur {
		fmt.Fprintf(u.Stderr, "gander %s is already installed\n", u.Current)
		return u.relink()
	}

	name := release.GanderAsset(u.Target)
	want256, err := u.Client.SumFor(ctx, tag, name)
	if err != nil {
		return err
	}
	staged, err := tempPath(u.Home.BinDir(), ".gander-new-")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			os.Remove(staged)
		}
	}()
	if err := u.download(ctx, tag, name, staged, want256); err != nil {
		return err
	}
	if err := u.tryRun(ctx, staged, tag); err != nil {
		return err
	}
	if err := u.commit(staged); err != nil {
		return err
	}
	ok = true
	fmt.Fprintf(u.Stderr, "Updated gander from %s to %s\n", u.Current, tag)
	return nil
}

// download fetches name from release tag into a new executable file at dest, staged
// beside the binary it replaces: staging in /tmp fails across filesystems and when /tmp
// is noexec (deno#37116).
func (u *Updater) download(ctx context.Context, tag version.Version, name, dest string, want []byte) error {
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return fmt.Errorf("can't write in %s: %w", u.Home.BinDir(), err)
	}
	defer f.Close()
	url := u.Client.DownloadURL(tag, name)
	fmt.Fprintf(u.Stderr, "Downloading %s\n", url)
	got, n, err := u.Client.Download(ctx, url, f, nil)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("the download of %s doesn't match its checksum, so it was deleted:\n  expected sha256 %x (from %s)\n  got      sha256 %x over %d bytes\ntry again",
			url, want, release.SumsName, got, n)
	}
	// The mode passed to OpenFile goes through the umask (go-update#35).
	if err := f.Chmod(0o755); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

// tryRun runs the new binary before it replaces this one, and checks it reports tag. A
// build that can't run here, after a raised OS minimum, would otherwise leave a gander
// that can't update itself back (bun#13028, juliaup#1620).
func (u *Updater) tryRun(ctx context.Context, path string, tag version.Version) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, path, "version")
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if f := strings.Fields(out.String()); err == nil && len(f) == 2 && f[0] == "gander" && f[1] == tag.String() {
		return nil
	}
	why := strings.TrimSpace(out.String() + errOut.String())
	if err != nil {
		why = strings.TrimSpace(err.Error() + ": " + why)
	}
	return fmt.Errorf("gander %s doesn't run on this system (%s); you're still on %s", tag, why, u.Current)
}
