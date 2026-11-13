# gander

A version manager for the [Goose](https://github.com/aardappel/goose) compiler, in the spirit
of rustup and nvm. gander installs Goose versions side by side, and puts a `goose` on your PATH
that runs the version each project asks for.

## Install

On macOS (arm64 or x64) or Linux (x64):

```sh
curl -fsSL https://github.com/bgourlie/gander/releases/latest/download/install.sh | sh
```

This puts `gander` and `goose` in `~/.gander/bin`, adds that folder to PATH in your shell's
startup files, and installs the latest Goose. Open a new terminal, or run
`. "$HOME/.gander/env"`, to use them.

- To leave your startup files alone: `curl ... | sh -s -- --no-modify-path`.
- `GANDER_HOME=/some/folder` installs there instead; `GANDER_VERSION=v0.1.0` installs that
  release of gander.

Windows support (x64, or Arm from Windows 11) is in a release candidate, v0.3.0-rc.1. The
latest release has no Windows build yet, so install the release candidate by name, in
PowerShell:

```powershell
$env:GANDER_VERSION = 'v0.3.0-rc.1'
irm https://github.com/bgourlie/gander/releases/download/v0.3.0-rc.1/install.ps1 | iex
```

Once Windows support is in a full release, this will be:

```powershell
irm https://github.com/bgourlie/gander/releases/latest/download/install.ps1 | iex
```

This puts `gander.exe` and `goose.exe` in `%LOCALAPPDATA%\gander\bin`, adds that folder to your
PATH, and installs the latest Goose. They work in that PowerShell window straight away, and in
terminals and editors started afterwards.

- To leave PATH alone: `& ([scriptblock]::Create((irm .../install.ps1))) --no-modify-path`.
- `GANDER_HOME` and `GANDER_VERSION` work as above: `$env:GANDER_HOME = 'D:\gander'`.
- Windows searches the system PATH before yours, so a `goose` installed for all users comes
  first. `gander doctor` names it.

**Microsoft Defender** currently flags `gander.exe` as a virus (`Trojan:Win32/Bearfoos.A!ml`)
and quarantines it after the install. The detection is a false positive, and I'm working on a
way to address it.

### In GitHub Actions and Docker

**GitHub Actions:** each step runs in a new shell that doesn't read your startup files, so the
installer also adds `~/.gander/bin` to the job's PATH. `goose` works from the next step on,
though not in the step that installs it. Give the job `GITHUB_TOKEN`, too: without a token,
GitHub's API answers 60 requests an hour per IP address, and runners that share addresses,
such as GitHub's macOS runners, can find that used up.

```yaml
env:
  GITHUB_TOKEN: ${{ github.token }}
steps:
  - run: curl -fsSL https://github.com/bgourlie/gander/releases/latest/download/install.sh | sh
  - run: goose version
```

On a Windows runner, run `install.ps1` instead, with `shell: pwsh`.

**Docker:** `RUN` lines don't read startup files either, so put the folder on PATH yourself with
`ENV`. It's in the home folder of the user who ran the installer, which is `/root` unless your
Dockerfile switches users. Goose's Linux compiler needs glibc 2.35 or later, so use an image
such as `ubuntu:24.04` or `debian:12` rather than Alpine. On an Apple silicon Mac, build with
`--platform linux/amd64`, since Goose has no Linux arm64 compiler.

```dockerfile
FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl
RUN curl -fsSL https://github.com/bgourlie/gander/releases/latest/download/install.sh | sh
ENV PATH="/root/.gander/bin:$PATH"
```

### Without the script

**With Go:** `go install github.com/bgourlie/gander@latest`, then `gander self install` to set
up `~/.gander` and PATH.

**By hand:** download your platform's binary and `SHA256SUMS` from the
[releases page](https://github.com/bgourlie/gander/releases), check the binary, and run
`self install` from it:

```sh
grep ' gander-macos-arm64$' SHA256SUMS | shasum -a 256 -c   # sha256sum -c on Linux
chmod +x gander-macos-arm64
./gander-macos-arm64 self install
```

On macOS, a file downloaded in a browser is quarantined, and macOS won't run it because gander
isn't notarized by Apple. Clear the quarantine first with
`xattr -d com.apple.quarantine gander-macos-arm64`. The install script doesn't need this, since
curl doesn't quarantine what it downloads.

On Windows, compare `(Get-FileHash gander-windows-x64.exe).Hash` with its line in `SHA256SUMS`,
then run `.\gander-windows-x64.exe self install`. gander isn't code-signed, so SmartScreen warns
about a copy downloaded in a browser; `Unblock-File gander-windows-x64.exe` clears that.
install.ps1's download isn't marked, so it doesn't need this.

## Use

```sh
gander install v2026.4   # install a version (or "latest")
gander pin v2026.4       # use it in this folder and below, via .goose-version
gander default v2026.4   # use it everywhere else
gander list              # installed versions; --available for every release
gander current           # the version in use here, and why
gander doctor            # check that goose can build here, and what to fix if not
```

`goose` picks its version from, highest first: a leading `+version` argument
(`goose +v2026.3 build app.goose`), `GANDER_GOOSE`, the nearest `.goose-version`, or the
default. A version named by one of the first three is installed the first time it's used.

`gander help` lists everything.

## Update and uninstall

```sh
gander self update       # the latest gander
gander self uninstall    # gander, its PATH setup, and every Goose version
```
