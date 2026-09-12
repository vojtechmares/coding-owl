#!/bin/bash
#
# Builds the desktop release artifacts for one version: the signed app bundle in
# a zip, and a checksum file beside it. The release workflow runs this on a mac,
# so what ships is what a developer can build from the same tag.
#
#   ./scripts/build-desktop-release.sh v0.1.0
#
# The app is ad-hoc signed and not notarised (ADR-0010). Gatekeeper will not
# launch a quarantined bundle it cannot verify, which is what the cask's
# postflight step is for; signing is still worth doing, because an unsigned
# arm64 bundle will not launch at all.
#
# Environment:
#   DIST   where the artifacts are written (default: dist)
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# darwin/arm64 is the only MVP platform (ADR-0010). The zip is named the way the
# tap's other cask names its asset: <Product>-<version>-<arch>.zip.
APP="Coding Owl.app"
TARGET_ARCH="arm64"
DIST="${DIST:-$ROOT/dist}"

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

VERSION="${1:-}"
[[ -n "$VERSION" ]] || die "usage: build-desktop-release.sh <version>, for example v0.1.0"
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] ||
	die "'$VERSION' is not a version like v0.1.0"

[[ "$(uname -s)" == "Darwin" ]] || die "the desktop app is built on macOS only"
command -v wails >/dev/null || die "the wails CLI is not installed; go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0"

# Without the leading v: that belongs to the tag, not to what the app reports.
NUMBER="${VERSION#v}"
STAMP="github.com/vojtechmares/coding-owl/internal/version.Version=$NUMBER"
ZIP="CodingOwl-$NUMBER-$TARGET_ARCH.zip"

echo "==> Building $APP $VERSION for darwin/$TARGET_ARCH"
(cd cmd/owl-desktop && wails build -clean -platform "darwin/$TARGET_ARCH" -ldflags "-X $STAMP")
# The frontend build empties dist, and go:embed needs something there on a
# fresh checkout.
touch "$ROOT/cmd/owl-desktop/frontend/dist/.gitkeep"

BUILT="$ROOT/cmd/owl-desktop/build/bin/owl-desktop.app"
[[ -d "$BUILT" ]] || die "wails built no app bundle at $BUILT"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

# Named for what a person sees in their applications folder rather than for the
# binary inside it. The bundle's directory name and its executable are separate
# things, and CFBundleExecutable still says owl-desktop.
cp -R "$BUILT" "$STAGE/$APP"

# The version is written into the copy rather than into wails.json: the tag is
# the only source of truth for it, and nothing in the tree should have to be
# bumped - or put back afterwards - to cut a release.
echo "==> Stamping $NUMBER into the bundle"
PLIST="$STAGE/$APP/Contents/Info.plist"
for KEY in CFBundleShortVersionString CFBundleVersion; do
	/usr/libexec/PlistBuddy -c "Set :$KEY $NUMBER" "$PLIST" ||
		die "the version could not be stamped into $KEY"
done

# Ad-hoc, and over the whole bundle, and after the version is in: a signature
# covers Info.plist, so signing first would leave one that no longer matches.
# --force so that re-signing what wails signed is not an error.
echo "==> Signing $APP"
/usr/bin/codesign --force --deep --sign - --timestamp=none "$STAGE/$APP"
/usr/bin/codesign --verify --deep --strict "$STAGE/$APP" ||
	die "the signed bundle does not verify"

mkdir -p "$DIST"
echo "==> Packaging $ZIP"
# ditto rather than zip: it keeps the bundle's symbolic links and its signature,
# and it is what Homebrew's cask will unpack.
rm -f "$DIST/$ZIP"
/usr/bin/ditto -c -k --sequesterRsrc --keepParent "$STAGE/$APP" "$DIST/$ZIP"

echo "==> Checksumming"
(cd "$DIST" && shasum -a 256 "$ZIP" >"$ZIP.sha256")
cat "$DIST/$ZIP.sha256"
