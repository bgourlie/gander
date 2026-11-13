package self

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/bgourlie/gander/internal/fsutil"
)

// othersFirst ends the note about another goose on PATH. Windows puts the user's PATH
// after the system's, so a goose on the system PATH wins.
const othersFirst = "in new terminals, gander's comes first unless that one is on the system PATH"

// userEnvKey is the key under HKEY_CURRENT_USER that holds the user's environment
// variables. Tests point it elsewhere.
var userEnvKey = "Environment"

const editPathHint = `search the Start menu for "Edit environment variables for your account"`

func (e *Env) canModifyPath() error { return nil }

// writeEnv has nothing to write: Windows keeps PATH in the registry.
func (e *Env) writeEnv() error { return nil }

func (e *Env) pathByHand() string {
	return fmt.Sprintf("Left PATH alone; to put gander on it, add %s to your PATH: %s\n", e.Home.BinDir(), editPathHint)
}

// addToPath puts gander's folder first in the user's PATH.
func (e *Env) addToPath() {
	bin := e.Home.BinDir()
	changed, err := editUserPath(func(v string) (string, bool) { return pathWith(v, bin, expand) })
	switch {
	case err != nil:
		fmt.Fprintf(e.Stderr, "gander: warning: couldn't add %s to your PATH (%v); add it yourself: %s\n", bin, err, editPathHint)
	case changed:
		fmt.Fprintf(e.Stderr, "Added %s to your PATH\n", bin)
	}
}

func (e *Env) removeFromPath(warn func(format string, args ...any)) {
	bin := e.Home.BinDir()
	changed, err := editUserPath(func(v string) (string, bool) { return pathWithout(v, bin, expand) })
	switch {
	case err != nil:
		warn("couldn't remove %s from your PATH (%v); remove it yourself: %s", bin, err, editPathHint)
	case changed:
		fmt.Fprintf(e.Stderr, "Removed %s from your PATH\n", bin)
	}
}

// NextSteps says how to use gander after installing it. Terminals that were already
// open keep the PATH they started with; install.ps1 updates the one it ran in.
func (e *Env) NextSteps(modifiedPath bool) string {
	if !modifiedPath {
		return ""
	}
	return "Open a new terminal to use gander and goose; editors find them once restarted.\n"
}

// editUserPath changes the user's PATH with edit, and tells Windows. It reads and writes
// the registry value raw: .NET's environment methods expand %VARS% into it for good,
// and setx cuts it at 1024 characters (nvm-windows#546). A value that isn't a string
// is left alone, with an error.
func editUserPath(edit func(string) (string, bool)) (bool, error) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, userEnvKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	val, typ, err := k.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		val, typ, err = "", registry.EXPAND_SZ, nil
	}
	if err != nil {
		return false, err
	}
	val, changed := edit(val)
	if !changed {
		return false, nil
	}
	if typ == registry.SZ {
		err = k.SetStringValue("Path", val)
	} else {
		err = k.SetExpandStringValue("Path", val)
	}
	if err != nil {
		return false, err
	}
	broadcastEnvironment()
	return true, nil
}

func expand(s string) string {
	if x, err := registry.ExpandString(s); err == nil {
		return x
	}
	return s
}

var sendMessageTimeout = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")

// broadcastEnvironment tells Explorer, and so the programs it starts from now on, to
// read the environment again. SMTO_ABORTIFHUNG with a 5 second cap, as rustup does, so
// a hung window can't stall the install.
func broadcastEnvironment() {
	const hwndBroadcast, wmSettingChange, smtoAbortIfHung = 0xffff, 0x001a, 0x0002
	name, _ := windows.UTF16PtrFromString("Environment")
	sendMessageTimeout.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(name)), smtoAbortIfHung, 5000, 0)
}

// replace renames src over dst. Windows won't replace a running program, or any other
// name of its file, such as goose.exe while an editor runs "goose lsp" (rustup#1367),
// but it will rename one. So dst moves aside first, and removeOld deletes it once it
// stops.
func replace(src, dst string) error {
	aside, err := tempPath(filepath.Dir(dst), "."+strings.TrimSuffix(filepath.Base(dst), ".exe")+"-old-*")
	if err != nil {
		return err
	}
	if err := fsutil.Rename(dst, aside); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := fsutil.Rename(src, dst); err != nil {
		fsutil.Rename(aside, dst)
		return err
	}
	os.Remove(aside)
	return nil
}

// discard deletes p, a program in <home>/bin. Windows won't delete a running program,
// and the gander removing itself is running, so such a file moves out of gander's home
// into the temp folder, where Disk Cleanup deletes it. A temp folder on another drive
// leaves it in place, with the error.
func discard(p string) error {
	err := os.Remove(p)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if tmp, terr := tempPath(os.TempDir(), "gander-removed-*"); terr == nil && os.Rename(p, tmp) == nil {
		return nil
	}
	return err
}
