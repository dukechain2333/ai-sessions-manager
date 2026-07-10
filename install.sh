#!/bin/sh
# Install sm (ai-sessions-manager) on macOS or Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/dukechain2333/ai-sessions-manager/main/install.sh | sh
#
# Options (pass after `| sh -s --`, or as env vars):
#   --version vX.Y.Z   install a specific release   (env: VERSION)
#   --bin DIR          install into DIR             (env: BINDIR, default ~/.local/bin)
#
set -eu

OWNER="dukechain2333"
REPO="ai-sessions-manager"
BINARY="sm"
VERSION="${VERSION:-}"
BINDIR="${BINDIR:-$HOME/.local/bin}"

while [ $# -gt 0 ]; do
	case "$1" in
		--version) VERSION="$2"; shift 2 ;;
		--bin)     BINDIR="$2";  shift 2 ;;
		-h|--help)
			sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
			exit 0 ;;
		*) echo "unknown option: $1" >&2; exit 1 ;;
	esac
done

err() { echo "install: $*" >&2; exit 1; }

# --- detect platform -------------------------------------------------------
os="$(uname -s)"
case "$os" in
	Linux)  os="linux" ;;
	Darwin) os="darwin" ;;
	*) err "unsupported OS: $os (this tool supports Linux and macOS)" ;;
esac

arch="$(uname -m)"
case "$arch" in
	x86_64|amd64)  arch="amd64" ;;
	arm64|aarch64) arch="arm64" ;;
	*) err "unsupported architecture: $arch" ;;
esac

command -v curl >/dev/null 2>&1 || err "curl is required"
command -v tar  >/dev/null 2>&1 || err "tar is required"

# --- resolve version (follow the /releases/latest redirect) ----------------
if [ -z "$VERSION" ]; then
	latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
		"https://github.com/$OWNER/$REPO/releases/latest")" \
		|| err "could not reach GitHub to find the latest release"
	VERSION="${latest_url##*/}"
fi
[ -n "$VERSION" ] && [ "$VERSION" != "releases" ] \
	|| err "no releases found for $OWNER/$REPO yet"

ver="${VERSION#v}"   # strip leading v for the asset name
asset="${BINARY}_${ver}_${os}_${arch}.tar.gz"
base="https://github.com/$OWNER/$REPO/releases/download/$VERSION"

echo "install: $BINARY $VERSION ($os/$arch)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "install: downloading $asset"
curl -fsSL "$base/$asset"        -o "$tmp/$asset"      || err "download failed: $base/$asset"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" || err "could not download checksums.txt"

# --- verify checksum -------------------------------------------------------
want="$(grep " $asset\$" "$tmp/checksums.txt" | awk '{print $1}')"
[ -n "$want" ] || err "no checksum listed for $asset"
if command -v sha256sum >/dev/null 2>&1; then
	got="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
	got="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
else
	err "need sha256sum or shasum to verify the download"
fi
[ "$want" = "$got" ] || err "checksum mismatch for $asset (expected $want, got $got)"

# --- install ---------------------------------------------------------------
tar -xzf "$tmp/$asset" -C "$tmp" "$BINARY" || err "could not extract $BINARY"
chmod +x "$tmp/$BINARY"

mkdir -p "$BINDIR" 2>/dev/null || true
if [ -w "$BINDIR" ]; then
	mv "$tmp/$BINARY" "$BINDIR/$BINARY"
elif command -v sudo >/dev/null 2>&1; then
	echo "install: $BINDIR needs elevated permissions; using sudo"
	sudo mkdir -p "$BINDIR"
	sudo mv "$tmp/$BINARY" "$BINDIR/$BINARY"
else
	err "cannot write to $BINDIR (set --bin DIR to a writable location)"
fi

echo "install: installed $BINARY to $BINDIR/$BINARY"

# --- PATH hint -------------------------------------------------------------
case ":$PATH:" in
	*":$BINDIR:"*) echo "install: run '$BINARY' to start" ;;
	*) echo "install: add $BINDIR to your PATH, e.g."
	   echo "  echo 'export PATH=\"$BINDIR:\$PATH\"' >> ~/.profile" ;;
esac
