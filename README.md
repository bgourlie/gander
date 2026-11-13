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
- In GitHub Actions, later steps find `goose` on PATH. In a Dockerfile, `RUN` lines read no
  startup files, so add `ENV PATH="/root/.gander/bin:$PATH"` after installing.
- With Go: `go install github.com/bgourlie/gander@latest`, then `gander self install` to set
  up `~/.gander` and PATH.
- If you download a binary in a browser on macOS, clear its quarantine before running it:
  `xattr -d com.apple.quarantine gander-macos-arm64`.

Windows is coming.

## Use

```sh
gander install v2026.4   # install a version (or "latest")
gander pin v2026.4       # use it in this folder and below, via .goose-version
gander default v2026.4   # use it everywhere else
gander list              # installed versions; --available for every release
gander current           # the version in use here, and why
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
