#!/bin/sh
# molt installer for Linux and macOS.
#
#   curl -sSL https://raw.githubusercontent.com/jossssdl/molt/main/install.sh | sh
#
# Downloads the latest release from GitHub, verifies its SHA-256 checksum and
# installs the binary into /usr/local/bin (when writable) or ~/.local/bin.
#
# Optional environment variables:
#   MOLT_VERSION       version to install, e.g. 0.1.0 (default: latest release)
#   MOLT_INSTALL_DIR   install directory (default: /usr/local/bin, else ~/.local/bin)
#   MOLT_BASE_URL      release download base URL (default: GitHub releases; for testing)
#   MOLT_OS, MOLT_ARCH override the detected platform (linux|darwin, amd64|arm64)
#
# The whole script is wrapped in main() and only runs on the last line, so a
# truncated download can never execute half of it.

set -eu

REPO="jossssdl/molt"
BINARY="molt"

say() { printf '%s\n' "$*"; }
die() { printf 'molt-install: error: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

detect_os() {
    if [ -n "${MOLT_OS:-}" ]; then printf '%s' "$MOLT_OS"; return; fi
    case "$(uname -s)" in
        Linux)  printf 'linux' ;;
        Darwin) printf 'darwin' ;;
        *) die "unsupported operating system '$(uname -s)'. On Windows download the .zip from https://github.com/$REPO/releases or run: go install github.com/$REPO/cmd/molt@latest" ;;
    esac
}

detect_arch() {
    if [ -n "${MOLT_ARCH:-}" ]; then printf '%s' "$MOLT_ARCH"; return; fi
    case "$(uname -m)" in
        x86_64 | amd64)  printf 'amd64' ;;
        arm64 | aarch64) printf 'arm64' ;;
        *) die "unsupported architecture '$(uname -m)' (supported: amd64, arm64)" ;;
    esac
}

# latest_version prints the newest release number without a leading "v". It
# follows the /releases/latest redirect, which needs no API token and is not
# subject to API rate limits.
latest_version() {
    url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")" ||
        die "could not reach GitHub to find the latest release"
    tag="${url##*/}"
    case "$tag" in
        v[0-9]*) printf '%s' "${tag#v}" ;;
        *) die "no published release found at https://github.com/$REPO/releases" ;;
    esac
}

sha256_of() {
    if have sha256sum; then
        sha256sum "$1" | cut -d' ' -f1
    elif have shasum; then
        shasum -a 256 "$1" | cut -d' ' -f1
    else
        die "need sha256sum or shasum to verify the download"
    fi
}

choose_install_dir() {
    if [ -n "${MOLT_INSTALL_DIR:-}" ]; then
        printf '%s' "$MOLT_INSTALL_DIR"
    elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
        printf '/usr/local/bin'
    else
        printf '%s/.local/bin' "$HOME"
    fi
}

main() {
    have curl || die "curl is required"
    have tar || die "tar is required"

    os="$(detect_os)"
    arch="$(detect_arch)"
    version="${MOLT_VERSION:-}"
    version="${version#v}"
    [ -n "$version" ] || version="$(latest_version)"

    archive="${BINARY}_${version}_${os}_${arch}.tar.gz"
    base="${MOLT_BASE_URL:-https://github.com/$REPO/releases/download/v$version}"

    tmp="$(mktemp -d)" || die "could not create a temporary directory"
    trap 'rm -rf "$tmp"' EXIT INT TERM

    say "Installing molt $version ($os/$arch)"
    curl -fsSL "$base/$archive" -o "$tmp/$archive" || die "download failed: $base/$archive"
    curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" || die "download failed: $base/checksums.txt"

    expected="$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")"
    [ -n "$expected" ] || die "$archive is not listed in checksums.txt"
    actual="$(sha256_of "$tmp/$archive")"
    [ "$expected" = "$actual" ] || die "checksum mismatch for $archive (expected $expected, got $actual); nothing was installed"
    say "Checksum OK"

    tar -xzf "$tmp/$archive" -C "$tmp" "$BINARY" || die "could not extract $BINARY from $archive"

    dir="$(choose_install_dir)"
    mkdir -p "$dir" || die "cannot create $dir (set MOLT_INSTALL_DIR to another location)"
    [ -w "$dir" ] || die "$dir is not writable (set MOLT_INSTALL_DIR, or re-run with sudo)"
    cp "$tmp/$BINARY" "$dir/$BINARY.new" && chmod 0755 "$dir/$BINARY.new" && mv -f "$dir/$BINARY.new" "$dir/$BINARY" ||
        die "could not install into $dir"

    say "Installed $dir/$BINARY"
    case ":$PATH:" in
        *":$dir:"*) ;;
        *) say "Note: $dir is not in your PATH. Add it, for example: export PATH=\"$dir:\$PATH\"" ;;
    esac
    say "Run 'molt -version' to check, then 'molt -root <your projects folder>'."
}

main "$@"
