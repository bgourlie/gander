#!/bin/sh
# Installs gander, a version manager for the Goose compiler:
#
#     curl -fsSL https://github.com/bgourlie/gander/releases/latest/download/install.sh | sh
#
# It downloads gander for this machine from gander's latest GitHub release, checks it
# against the release's SHA256SUMS, and runs "gander self install", which puts gander
# and goose in ~/.gander/bin and adds that folder to PATH in your shell's startup files.
# Arguments go to "gander self install":
#
#     curl -fsSL .../install.sh | sh -s -- --no-modify-path
#
# GANDER_HOME changes the folder; GANDER_VERSION installs that release instead of the
# latest.
#
# Everything is in functions, so a download cut short runs nothing.

set -eu

repo=https://github.com/bgourlie/gander

say() { printf '%s\n' "$*" >&2; }
fail() {
	say "gander: $*"
	exit 1
}

# target names this machine's build of gander the way its releases do. It refuses
# anything else before touching the network: a guessed build fails later with "Exec
# format error". uname's exact strings, with no tr, which OpenWRT's mangles.
target() {
	os=$(uname -s) || fail "can't run uname"
	arch=$(uname -m) || fail "can't run uname"
	case $os in
	Darwin)
		# A shell under Rosetta says x86_64; ask the hardware (rustup#3063).
		if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
			arch=arm64
		fi
		case $arch in
		arm64) echo macos-arm64 ;;
		x86_64) echo macos-x64 ;;
		*) unsupported "macOS $arch" ;;
		esac
		;;
	Linux)
		case $arch in
		x86_64) echo linux-x64 ;;
		*) unsupported "Linux $arch" ;;
		esac
		;;
	*) unsupported "$os $arch" ;;
	esac
}

unsupported() {
	say "gander: there's no build for $1, because Goose ships no compiler for it."
	say "Builds exist for macOS (arm64 and x64) and Linux x64; Windows is coming."
	case $1 in
	Linux\ aarch64 | Linux\ arm64) say "In Docker on an Apple silicon Mac, use --platform linux/amd64." ;;
	esac
	exit 1
}

# fetch downloads $1 into $2. The shell opens $2, not curl: Ubuntu's snap-packaged curl
# can't write into a hidden folder such as ~/.gander (rustup#2948).
fetch() {
	if ! curl -fsSL --retry 3 --proto '=https' "$1" >"$2"; then
		fail "can't download $1"
	fi
}

# latest_tag reads the latest release's tag from where releases/latest redirects, so
# the binary and SHA256SUMS come from the same release even if one is published
# meanwhile.
latest_tag() {
	url=$(curl -fsS --retry 3 --proto '=https' -o /dev/null -w '%{redirect_url}' "$repo/releases/latest") ||
		fail "can't reach $repo/releases/latest"
	case $url in
	*/releases/tag/?*) echo "${url##*/releases/tag/}" ;;
	*) fail "can't find gander's latest release at $repo/releases" ;;
	esac
}

# sha256 prints the SHA-256 of $1. Comparing hashes here, rather than with "sha256sum
# -c --ignore-missing", matters: macOS's returns 0 when it checked nothing.
sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sum=$(sha256sum "$1") || return 1
	elif command -v shasum >/dev/null 2>&1; then
		sum=$(shasum -a 256 "$1") || return 1
	else
		fail "needs sha256sum or shasum to check the download"
	fi
	echo "${sum%% *}"
}

main() {
	t=$(target) || exit 1
	asset=gander-$t
	command -v curl >/dev/null 2>&1 || fail "needs curl"
	# Through sudo, HOME may still be the user's, so root would own files there.
	if [ "$(id -u)" = 0 ] && [ -n "${SUDO_USER:-}" ]; then
		fail "don't install gander with sudo: it installs into your own home folder, so run it as yourself"
	fi
	home=${GANDER_HOME:-${HOME:+$HOME/.gander}}
	case $home in
	/*) ;;
	*) fail "set GANDER_HOME to a writable absolute folder (HOME isn't set)" ;;
	esac
	bin=$home/bin
	mkdir -p "$bin" || fail "set GANDER_HOME to a writable absolute folder; can't create $bin"

	if [ -n "${GANDER_VERSION:-}" ]; then
		tag=$GANDER_VERSION
	else
		tag=$(latest_tag) || exit 1
	fi
	say "Downloading gander $tag for $t"

	# Into gander's own folder, not /tmp, which may be noexec.
	file=$bin/.gander-install-$$
	sums=$bin/.gander-install-$$.sums
	trap 'rm -f "$file" "$sums"' EXIT
	fetch "$repo/releases/download/$tag/$asset" "$file"
	fetch "$repo/releases/download/$tag/SHA256SUMS" "$sums"

	want=
	while read -r h name; do
		if [ "$name" = "$asset" ] || [ "$name" = "*$asset" ]; then
			want=$h
		fi
	done <"$sums"
	[ -n "$want" ] || fail "release $tag's SHA256SUMS has no line for $asset"
	got=$(sha256 "$file") || fail "can't hash $file"
	if [ "$got" != "$want" ]; then
		fail "the download of $asset doesn't match release $tag's SHA256SUMS (expected $want, got $got); try again"
	fi
	chmod +x "$file"

	# The script itself is on stdin when piped to sh, so gander gets none. (Not $status,
	# which is read-only in zsh, where people pipe this too.)
	code=0
	"$file" self install "$@" </dev/null || code=$?
	if [ "$code" = 126 ]; then
		fail "can't run $file; if $bin is on a filesystem mounted noexec, set GANDER_HOME to a folder elsewhere"
	fi
	return "$code"
}

main "$@"
