#!/bin/sh
# CommHub installer.
#
#   curl -fsSL https://raw.githubusercontent.com/Neha611/commhub/main/install.sh | sh
#
# Environment:
#   COMMHUB_VERSION      tag to install (default: latest release)
#   COMMHUB_INSTALL_DIR  where to put the binary (default: ~/.local/bin)
set -eu

REPO="Neha611/commhub"
VERSION="${COMMHUB_VERSION:-}"
INSTALL_DIR="${COMMHUB_INSTALL_DIR:-$HOME/.local/bin}"

info() { printf '  %s\n' "$*"; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed"; }
need uname
need mktemp
command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 \
  || die "curl or wget is required"

fetch() { # fetch <url> <dest>
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  else
    wget -qO "$2" "$1"
  fi
}

case "$(uname -s)" in
  Linux)  OS=linux ;;
  Darwin) OS=macos ;;
  *) die "unsupported operating system: $(uname -s). Windows users: download the .zip from https://github.com/$REPO/releases" ;;
esac

case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

if [ -z "$VERSION" ]; then
  info "Finding the latest release…"
  TMPJSON=$(mktemp)
  fetch "https://api.github.com/repos/$REPO/releases/latest" "$TMPJSON" \
    || die "could not reach the GitHub API"
  VERSION=$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$TMPJSON" | head -1)
  rm -f "$TMPJSON"
  [ -n "$VERSION" ] || die "no published release found for $REPO"
fi
NUM="${VERSION#v}"

ARCHIVE="commhub_${NUM}_${OS}_${ARCH}.tar.gz"
BASE="https://github.com/$REPO/releases/download/$VERSION"

TMP=$(mktemp -d)
# 0700, and cleaned up on any exit path.
chmod 700 "$TMP"
trap 'rm -rf "$TMP"' EXIT INT TERM

info "Downloading commhub $VERSION ($OS/$ARCH)…"
fetch "$BASE/$ARCHIVE" "$TMP/$ARCHIVE" || die "no archive named $ARCHIVE in release $VERSION"
fetch "$BASE/checksums.txt" "$TMP/checksums.txt" || die "could not download checksums.txt"

# Verify before extracting. An unverified archive is never unpacked.
info "Verifying checksum…"
EXPECTED=$(grep " $ARCHIVE\$" "$TMP/checksums.txt" | awk '{print $1}')
[ -n "$EXPECTED" ] || die "$ARCHIVE is not listed in checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "$TMP/$ARCHIVE" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL=$(shasum -a 256 "$TMP/$ARCHIVE" | awk '{print $1}')
else
  die "need sha256sum or shasum to verify the download"
fi
[ "$ACTUAL" = "$EXPECTED" ] || die "checksum mismatch — expected $EXPECTED, got $ACTUAL. Do not use this file."
info "Checksum OK"

# A checksum proves the archive matches the release page. The attestation proves
# the release page itself came from this repository's CI. Verified when the
# GitHub CLI is available; skipped, with a note, when it is not.
if command -v gh >/dev/null 2>&1; then
  if gh attestation verify "$TMP/$ARCHIVE" --repo "$REPO" >/dev/null 2>&1; then
    info "Build attestation OK — this archive was built by $REPO CI"
  else
    info "Note: could not verify the build attestation (needs 'gh auth login')"
  fi
fi

tar xzf "$TMP/$ARCHIVE" -C "$TMP"
[ -f "$TMP/commhub" ] || die "archive did not contain a commhub binary"

mkdir -p "$INSTALL_DIR"
install -m 755 "$TMP/commhub" "$INSTALL_DIR/commhub" 2>/dev/null \
  || { cp "$TMP/commhub" "$INSTALL_DIR/commhub" && chmod 755 "$INSTALL_DIR/commhub"; }

printf '\n  Installed commhub %s to %s\n\n' "$VERSION" "$INSTALL_DIR/commhub"

case ":$PATH:" in
  *":$INSTALL_DIR:"*)
    printf '  Run it:  commhub\n\n' ;;
  *)
    printf '  %s is not on your PATH. Add it:\n\n' "$INSTALL_DIR"
    printf '    echo '\''export PATH="$PATH:%s"'\'' >> ~/.bashrc && source ~/.bashrc\n\n' "$INSTALL_DIR"
    printf '  Or run it directly:  %s/commhub\n\n' "$INSTALL_DIR" ;;
esac

printf '  First run opens a welcome screen where you pick what to connect.\n'
printf '  Connecting Google needs a one-time Cloud project setup; the wizard\n'
printf '  walks you through it, and asks for two read-only scopes.\n\n'
