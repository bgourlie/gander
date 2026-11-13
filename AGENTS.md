# gander

A version manager for the [Goose](https://github.com/aardappel/goose) compiler, in the spirit of
rustup, nvm and zvm. One static Go binary for macOS (arm64, x64), Linux (x64) and Windows (x64):
the platforms Goose ships a compiler for.

## How it works

gander is one binary with two modes, chosen by the name it was run under.

- **As `gander`** it manages versions.
- **As `goose`** (a hardlink to the same binary in `<home>/bin`, or a copy where a hardlink
  can't be made) it is a proxy: it picks a version, installs it if allowed, and runs that
  version's compiler. Editors, scripts and CI run plain `goose` and get the version the
  project asks for.

The mode comes from `filepath.Base(os.Args[0])` without `.exe`, compared case-insensitively
with `goose`. Never from `os.Executable`.

### Choosing a version

Highest first:

1. A leading `+<version>` argument: `goose +v2026.3 check app.goose`. It applies to that one
   command and is never exported to the child.
2. `GANDER_GOOSE`.
3. The nearest `.goose-version`, walking up from the physical current directory
   (`filepath.EvalSymlinks(os.Getwd())`) to the filesystem root.
4. The default, set by `gander default`, kept in `<home>/default`.

One resolver returns the version, the reason it was chosen and the compiler's path. The proxy,
`current` and `doctor` all use it.

A version file (`.goose-version` and `<home>/default`) holds one exact version, the `v`
optional. A UTF-8 BOM is stripped, UTF-16 is decoded by its BOM, CR and surrounding whitespace
are trimmed, blank lines and `#` comments are skipped. Anything else is an error that names the
file; a file that can't be read never falls through to the next source.

Versions are Goose's release tags (`v2026.4`; the release workflow allows `v1.2.3` and
`v2.0-rc1` too). Compare numeric parts as numbers; a suffix sorts below its release. `latest`
is the newest release with no draft flag, no prerelease flag and no suffix, computed from the
release list.

### Auto-install

Only in proxy mode, and only for a version named by `+version`, `GANDER_GOOSE` or a
`.goose-version`. A missing default is an error, never a reinstall, and an auto-install never
changes the default. `GANDER_AUTO_INSTALL=0` turns it off. Progress goes to stderr, and only
when stderr is a terminal.

### Environment

`GANDER_HOME`, `GANDER_GOOSE` and `GANDER_AUTO_INSTALL=0`; the install scripts also read
`GANDER_VERSION`, a gander release to install instead of the latest. `GITHUB_TOKEN`, when set,
goes with requests to GitHub's API and nowhere else. Without it the API allows 60 requests an
hour per IP address, which GitHub's macOS runners, sharing addresses, used up in gander's own
CI; that's why token support, once removed, came back.

### Commands

| Command | |
|---|---|
| `gander install [version]` | Installs it; with none, the version pinned here, else the latest. The first version installed becomes the default. |
| `gander pin [version]` | Writes `.goose-version` here, installing the version if needed; with none, pins the version in use. |
| `gander default <version>` | Sets the default, installing the version if needed. |
| `gander update` | Installs the latest and moves the default to it. Pins are untouched. |
| `gander list [--available]` | Installed versions, marking the default and the one in use here; `--available` lists every release. |
| `gander current` | The version in use here, why, and its path. |
| `gander dir` | The folder of the version in use (its `docs/`, `samples/`). |
| `gander uninstall <version>` | Removes it. Uninstalling the default clears the default. |
| `gander doctor` | Checks the version in use, which `goose` comes first on PATH, `GOOSE_STDLIB`/`GOOSE_TCCLIB`, the OS minimums, and the C compiler the version's `goose toolchain --json` reports; then builds and runs hello world with it. On macOS, a failed build is tried again with the selected developer tools' SDK, to name the fix when the default SDK doesn't suit them. Exits 1 on any problem. |
| `gander self install [--no-modify-path]` | Copies the running gander to `<home>/bin` with `goose` beside it, and puts that folder on PATH; installs the latest Goose if there's no default. `install.sh` runs it, and running it again repairs an install. |
| `gander self update [version]` | Updates the gander in `<home>/bin` to the latest release, never older unless a version is named. |
| `gander self uninstall [-y]` | Removes gander's PATH setup, then only what gander made in `<home>`, versions included. |

Say "version", never "toolchain": `goose toolchain` is Goose's command for the C compiler.

### On disk

```
<home>/                  ~/.gander, %LOCALAPPDATA%\gander on Windows; GANDER_HOME overrides
  bin/gander
  bin/goose              the same binary
  bin/.g*-new-*          a new gander or goose, staged here and renamed into place
  bin/.g*-old-*          on Windows, a gander or goose an update moved aside while it ran;
                         deleted by the next self install or update once it has stopped
  env                    sh that puts bin/ first on PATH; startup files source it
  default                one line: the default version
  versions/<tag>/        an unpacked release; complete only once .gander-installed exists
  versions/<tag>.lock    held while that version installs or uninstalls; never deleted
  versions/.tmp-<tag>_*/ staging for an install or removal; swept only under <tag>'s lock
  versions/.doctor/      the program doctor builds, at a fixed path so Goose reuses one
                         build folder in GOOSE_HOME; removed when doctor is done
```

`_` can't appear in a tag, so one version's staging pattern never matches another's.

gander leaves Goose's own `GOOSE_HOME` (`~/.goose`) alone; versions share it safely.

### Installing gander

- `install.sh` (POSIX sh, curl only) checks the platform against a fixed list, reads the
  latest tag from where `releases/latest` redirects, downloads `gander-<os>-<arch>` and
  `SHA256SUMS` from that tag, compares the hash, and runs `gander self install "$@"`.
- `install.ps1` does the same on Windows, for Windows PowerShell 5.1 and later, run with
  `irm | iex`. It is one script block that never calls `exit` (which would close the user's
  window), and finally puts `<home>\bin` on that session's PATH too. ASCII only: 5.1 reads a
  script without a BOM in the system code page.
- On Unix, startup files get a marked block that sources `<home>/env`, guarded with
  `if [ -f ... ]`: `.profile` always; `.bash_profile`, `.bash_login` and `.bashrc` if they
  exist; `.zshenv` and `.zshrc` if zsh is the shell or on PATH; fish gets
  `conf.d/gander.fish`. A file that can't be written is a warning.
- On Windows, `<home>\bin` goes first in the user's `Path` in `HKCU\Environment`, read and
  written raw so `%VARS%` and the value's type stay, then `WM_SETTINGCHANGE` is broadcast.
- `$GITHUB_PATH` gets `<home>/bin` too, on every OS.
- gander's own release assets are bare binaries named `gander-macos-arm64`, `gander-macos-x64`,
  `gander-linux-x64` and `gander-windows-x64.exe`, plus `SHA256SUMS`, `install.sh` and
  `install.ps1`. The install scripts and `self update` build these names, so they never change.

### Goose's releases

- Assets are `goose-<tag>-<os>-<arch>.tar.gz` (`.zip` for Windows) for `linux-x64`,
  `macos-arm64`, `macos-x64` and `windows-x64`, at
  `https://github.com/aardappel/goose/releases/download/<tag>/<asset>`.
- Each archive is one top-level directory with the same name as the asset. Inside:
  the `goose` executable, `stdlib/`, `tcclib/`, `pkg/`, `docs/`, `samples/`, `vscode/`, `man/`.
- The compiler finds `stdlib/`, `tcclib/` and `pkg/` beside its own executable as the OS
  reports it, not from argv[0]. The folder must stay whole; a copy of the binary alone fails.
- `goose version` prints `goose v2026.4 (runtime dce889723a23)`. Releases before v2026.4
  print `goose runtime 4d3da949e563`, with no version; the install check accepts that.
- Checksums: a `SHA256SUMS` asset where the release has one (not yet: proposed upstream),
  otherwise the GitHub API's per-asset `digest` (`sha256:...`).
- The Linux compiler needs glibc 2.35 or later; the macOS one, macOS 13.3 or later.

## Package layout

```
main.go          the mode from argv[0], and the //go:debug settings
internal/
  version/       parsing and ordering tags
  versionfile/   reading and writing .goose-version and <home>/default
  home/          the layout of <home>: the default, installed versions
  resolve/       the resolver: version, reason, compiler path
  platform/      the build for this machine, OS minimums, exec errors, and whether a
                 program is running (a file per OS)
  release/       GitHub's release list, checksums, downloads with retries
  install/       lock, download, verify, extract, check, commit; and uninstall
  lock/          per-version file locks (flock; LockFileEx on Windows)
  proxy/         gander as goose: Prepare picks and installs, then run_<os>.go execs
  cli/           gander's commands
  doctor/        gander doctor's checks, and on macOS the SDK retry
  self/          gander installing, updating and removing itself; on Unix the env script
                 and startup-file blocks, on Windows the PATH in the registry
  fsutil/        write a new file and rename it into place; renames and removals that
                 retry while Windows antivirus holds a file; finding programs on PATH
  term/          whether stderr is a terminal
  buildinfo/     gander's own version
  releasetest/   a fake GitHub for tests: an httptest server and small archives
testdata/fakegoose/  the fake compiler main_test.go builds
install.sh       the curl | sh installer
install.ps1      the irm | iex installer for Windows
.github/workflows/ci.yml        tests on each OS, then uses real Goose releases
.github/workflows/release.yml   builds and publishes a release when a v* tag is pushed
```

Not built yet: `update`. The Linux exec error translation and NixOS hint have never run.

## Build and test

Go 1.27 or later (`go.mod` says so; the build relies on 1.27's GODEBUG settings).

```bash
go build ./...
go vet ./...
GOOS=linux go vet ./...
GOOS=windows go vet ./...
go test -race ./...   # without -race on Windows, where it needs cgo
```

Vet for every OS gander ships for: code behind build tags for another OS isn't compiled
otherwise.

Tests never reach the network: installs are tested against a local `httptest` server
(`internal/releasetest`) that serves small fake release archives and a fake release list. That
fake compiler is a shell script, so the install, proxy, self, cli and doctor tests skip on
Windows. `main_test.go` instead builds `testdata/fakegoose` and hardlinks the test binary as
`goose` and `gander` (copying it where it can't), so the real proxy runs on every OS. The
`_windows_test.go` files cover the job object, the registry PATH, and replacing and removing
programs while they run.

CI (`ci.yml`) runs the tests on Linux, macOS and Windows on every push to a branch, then
installs gander with `self install` on each and uses Goose's real releases.

To check against Goose's real releases, build gander into a scratch folder, hardlink it as
`goose` beside itself, and set `GANDER_HOME` to a scratch folder too, never your own
`~/.gander`. Then run `gander install v2026.4`, `goose version`, and `goose +v2026.3 version`
from a folder with a `.goose-version`.

### Releasing

Push a tag `vX.Y.Z` and the release workflow tests, builds and publishes. Run the workflow by
hand for a dry run, and rehearse a change to it with an rc tag (`v0.1.0-rc.1`, published as a
prerelease) and `GANDER_VERSION=v0.1.0-rc.1` for `install.sh`. Go's checksum database
remembers every tag, and immutable releases lock them, so never move or reuse a tag; fix
forward.

### Tests

Fewer, better tests; coverage for its own sake isn't a goal.

- Assert outcomes: what got installed or written, exit codes, error types (`errors.As`), and
  that nothing was left behind.
- Don't assert the wording of messages, formats nothing reads (such as the marker's contents),
  or mechanics such as request counts and headers. These break when the code changes while
  the behavior is still right.
- Compare output exactly only where it's a contract: what the proxy passes through to stdout,
  `gander dir`, the contents of `.goose-version`.
- For an error, check only what it must name: the file, variable or version at fault, or the
  command to run.
- Don't test trivial getters, or the same behavior at several layers.

## Conventions

- **Dependencies:** the standard library and `golang.org/x/sys`. Ask before adding anything else.
- **A platform decision is justified where it's made.** Each workaround gets a short comment
  saying what breaks without it, in two to four lines, with an issue reference only where it
  carries weight. The repository holds no research notes or design documents.
- **The proxy writes nothing to stdout.** `goose lsp` speaks over it. Messages and progress go to
  stderr.
- **The proxy never searches PATH for the compiler** and never uses the network except to
  auto-install. It runs the version's absolute path: `syscall.Exec` on Unix; on Windows a child
  with stdio passed through and its exit code returned.
- **Never change a file in place.** Write a new file and rename it over the old one. The one
  exception is the user's shell startup files: gander appends to them and cuts its block out
  in place, because a rename would replace a symlink into their dotfiles with a plain file.
- **Errors say what to do next**, and name the file, version or platform at fault.
- **Build for gander's scale.** Goose's archives are 10–13 MB, and each is checked against its
  hash before it's unpacked. Before adding a safeguard, setting or fallback, name what breaks
  for gander without it; otherwise prefer the simpler code and a clear error. Resumable
  downloads, a lock timeout setting, a lock PID file, unpacking limits, a second glibc check
  and a `/releases/latest` fallback for Goose's releases were each built and then removed for
  this reason.
- **Output.** gander's commands print their results (`list`, `current`, `dir`) on stdout, and
  progress, warnings and errors on stderr; errors and warnings start with `gander`. gander's
  own exit codes are 0, 1 for a failure, 2 for a mistake on the command line, and 130 when
  interrupted; once the proxy has handed over, the exit code is goose's.
