package home

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Roaming profiles copy all of %USERPROFILE% except AppData\Local at every sign-in, which
// would be about 25 MB per version (uv#7008), so gander lives in %LOCALAPPDATA%.
func defaultDir() (string, error) {
	d, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "gander"), nil
}
