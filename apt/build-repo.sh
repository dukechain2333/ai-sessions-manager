#!/usr/bin/env bash
# Build (or refresh) a signed APT repository under $1.
#
#   apt/build-repo.sh <repo-root>
#
# Expects the .deb files to already be in <repo-root>/pool/main/ and a GPG
# secret key to be imported into the current keyring (used to sign the
# Release file). Regenerates the Packages indexes, the Release file, its
# signatures (InRelease + Release.gpg), and exports the public key to
# <repo-root>/public.key.
set -euo pipefail

ROOT="${1:?usage: build-repo.sh <repo-root>}"
DIST="${APT_DIST:-stable}"
COMP="main"
ARCHES="amd64 arm64"

cd "$ROOT"

for arch in $ARCHES; do
	mkdir -p "dists/$DIST/$COMP/binary-$arch"
	dpkg-scanpackages --arch "$arch" pool/ 2>/dev/null \
		> "dists/$DIST/$COMP/binary-$arch/Packages"
	gzip -9kf "dists/$DIST/$COMP/binary-$arch/Packages"
done

apt-ftparchive \
	-o APT::FTPArchive::Release::Origin="ai-sessions-manager" \
	-o APT::FTPArchive::Release::Label="ai-sessions-manager" \
	-o APT::FTPArchive::Release::Suite="$DIST" \
	-o APT::FTPArchive::Release::Codename="$DIST" \
	-o APT::FTPArchive::Release::Components="$COMP" \
	-o APT::FTPArchive::Release::Architectures="$ARCHES" \
	release "dists/$DIST" > "dists/$DIST/Release"

gpg --batch --yes --clearsign -o "dists/$DIST/InRelease" "dists/$DIST/Release"
gpg --batch --yes -abs        -o "dists/$DIST/Release.gpg" "dists/$DIST/Release"

gpg --armor --export > public.key

# A landing page + .nojekyll so GitHub Pages serves the tree verbatim.
touch .nojekyll
cat > index.html <<'HTML'
<!doctype html><meta charset="utf-8"><title>ai-sessions-manager apt repo</title>
<h1>ai-sessions-manager — APT repository</h1>
<p>Install <code>sm</code> on Debian/Ubuntu:</p>
<pre>
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://dukechain2333.github.io/ai-sessions-manager/public.key \
  | sudo gpg --dearmor -o /etc/apt/keyrings/ai-sessions-manager.gpg
echo "deb [signed-by=/etc/apt/keyrings/ai-sessions-manager.gpg] https://dukechain2333.github.io/ai-sessions-manager stable main" \
  | sudo tee /etc/apt/sources.list.d/ai-sessions-manager.list
sudo apt update
sudo apt install ai-sessions-manager
</pre>
<p>See <a href="https://github.com/dukechain2333/ai-sessions-manager">the project</a>.</p>
HTML

echo "apt repo built at $ROOT (dist=$DIST, arches: $ARCHES)"
