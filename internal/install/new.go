package install

import (
	"io"

	"github.com/bgourlie/gander/internal/buildinfo"
	"github.com/bgourlie/gander/internal/home"
	"github.com/bgourlie/gander/internal/platform"
	"github.com/bgourlie/gander/internal/release"
)

// New returns an installer for this machine.
func New(h home.Home, stderr io.Writer, stderrIsTerminal bool) (*Installer, error) {
	t, err := platform.Detect()
	if err != nil {
		return nil, err
	}
	return &Installer{
		Home:        h,
		Client:      release.NewClient(buildinfo.UserAgent()),
		Target:      t,
		CheckSystem: func() error { return platform.Check(t) },
		Stderr:      stderr,
		Progress:    stderrIsTerminal,
	}, nil
}
