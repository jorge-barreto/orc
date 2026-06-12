#!/bin/sh
# orc installer — fetch the latest (or a pinned) release binary from GitHub,
# verify it against the published checksums, and install it to a bin directory.
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/jorge-barreto/orc/main/scripts/install.sh | sh
#
# Environment overrides:
#   ORC_VERSION   Tag to install (e.g. v0.1.0). Defaults to the latest release.
#   ORC_BIN_DIR   Install directory. Defaults to /usr/local/bin, falling back
#                 to $HOME/.local/bin when /usr/local/bin is not writable.
#
# Only the platforms GoReleaser builds are supported: linux/darwin × amd64/arm64.

set -eu

REPO="jorge-barreto/orc"
BINARY="orc"

err() {
	printf 'error: %s\n' "$1" >&2
	exit 1
}

info() {
	printf '%s\n' "$1" >&2
}

# Pick a downloader. curl is preferred; fall back to wget.
if command -v curl >/dev/null 2>&1; then
	dl() { curl -fsSL "$1"; }
	dl_to() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	dl() { wget -qO- "$1"; }
	dl_to() { wget -qO "$2" "$1"; }
else
	err "need curl or wget to download orc"
fi

# Detect OS. GoReleaser uses lowercase goos values (linux, darwin).
os=$(uname -s)
case "$os" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) err "unsupported OS: $os (orc ships linux and darwin builds)" ;;
esac

# Detect arch and map to GoReleaser's goarch values (amd64, arm64).
arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) err "unsupported architecture: $arch (orc ships amd64 and arm64 builds)" ;;
esac

# Resolve the version to install.
version="${ORC_VERSION:-}"
if [ -z "$version" ]; then
	info "Resolving the latest orc release..."
	# Read the tag from the latest-release redirect target rather than the API,
	# so we don't burn an unauthenticated GitHub API rate-limit slot.
	version=$(dl "https://api.github.com/repos/$REPO/releases/latest" \
		| grep -m1 '"tag_name"' \
		| sed 's/.*"tag_name"[^"]*"\([^"]*\)".*/\1/') || true
	[ -n "$version" ] || err "could not determine the latest release tag (set ORC_VERSION=vX.Y.Z to pin one)"
fi

# GoReleaser strips the leading v from archive names but keeps it in the tag.
ver_nov=${version#v}
archive="${BINARY}_${ver_nov}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"

info "Installing $BINARY $version ($os/$arch)..."

# Work in a temp dir we always clean up.
tmp=$(mktemp -d 2>/dev/null || mktemp -d -t orc-install)
trap 'rm -rf "$tmp"' EXIT INT TERM

dl_to "$base/$archive" "$tmp/$archive" \
	|| err "failed to download $archive — does release $version include $os/$arch?"
dl_to "$base/checksums.txt" "$tmp/checksums.txt" \
	|| err "failed to download checksums.txt for $version"

# Verify the archive against the published checksum.
expected=$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')
[ -n "$expected" ] || err "$archive not listed in checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
else
	err "need sha256sum or shasum to verify the download"
fi

[ "$actual" = "$expected" ] || err "checksum mismatch for $archive (expected $expected, got $actual)"
info "Checksum verified."

tar -xzf "$tmp/$archive" -C "$tmp" "$BINARY" \
	|| err "failed to extract $BINARY from $archive"

# Choose an install directory. Honor ORC_BIN_DIR; otherwise prefer
# /usr/local/bin and fall back to ~/.local/bin when it is not writable.
bindir="${ORC_BIN_DIR:-}"
if [ -z "$bindir" ]; then
	if [ -w /usr/local/bin ] 2>/dev/null; then
		bindir=/usr/local/bin
	elif [ -d /usr/local/bin ] && [ "$(id -u)" -eq 0 ]; then
		bindir=/usr/local/bin
	else
		bindir="$HOME/.local/bin"
	fi
fi

mkdir -p "$bindir" || err "could not create install dir $bindir"
install -m 0755 "$tmp/$BINARY" "$bindir/$BINARY" 2>/dev/null \
	|| mv "$tmp/$BINARY" "$bindir/$BINARY" \
	|| err "could not install to $bindir (set ORC_BIN_DIR to a writable path, or re-run with sudo)"
chmod 0755 "$bindir/$BINARY" 2>/dev/null || true

info "Installed $BINARY to $bindir/$BINARY"

# Nudge the user if the install dir isn't on PATH.
case ":$PATH:" in
	*":$bindir:"*) ;;
	*) info "Note: $bindir is not on your PATH. Add it with:"
	   info "  export PATH=\"$bindir:\$PATH\"" ;;
esac

info "Run 'orc --version' to verify."
