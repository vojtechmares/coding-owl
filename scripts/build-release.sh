#!/bin/bash
#
# Builds the release artifacts for one version: the darwin/arm64 archive and a
# checksums file beside it. The release workflow runs this after the guard job,
# so what ships is what a developer can build from the same tag.
#
#   ./scripts/build-release.sh v0.1.0
#
# Environment:
#   DIST   where the artifacts are written (default: dist)
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# darwin/arm64 is the only MVP platform (ADR-0010). The archive is named the way
# the tap's other formulae name theirs: <formula>_<version>_<os>_<arch>.
NAME="coding-owl"
BINARY="owl"
TARGET_OS="darwin"
TARGET_ARCH="arm64"
DIST="${DIST:-$ROOT/dist}"

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

VERSION="${1:-}"
[[ -n "$VERSION" ]] || die "usage: build-release.sh <version>, for example v0.1.0"
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] ||
	die "'$VERSION' is not a version like v0.1.0"

# The tag is the only source of truth for the version, so nothing in the tree
# carries it and the build stamps it in. Without the leading v: that belongs to
# the tag, not to what the binary reports.
STAMP="github.com/vojtechmares/coding-owl/internal/version.Version=${VERSION#v}"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

echo "==> Building $BINARY $VERSION for $TARGET_OS/$TARGET_ARCH"
CGO_ENABLED=0 GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" \
	go build -trimpath -ldflags "-s -w -X $STAMP" -o "$STAGE/$BINARY" ./cmd/owl

ARCHIVE="${NAME}_${VERSION}_${TARGET_OS}_${TARGET_ARCH}.tar.gz"
mkdir -p "$DIST"

echo "==> Packaging $ARCHIVE"
tar -czf "$DIST/$ARCHIVE" -C "$STAGE" "$BINARY"

# shasum is macOS's and sha256sum is the GNU one; the workflow cross-compiles on
# Linux and a developer is on either.
echo "==> Checksumming"
cd "$DIST"
if command -v shasum >/dev/null; then
	shasum -a 256 "$ARCHIVE" >checksums.txt
else
	sha256sum "$ARCHIVE" >checksums.txt
fi
cat checksums.txt
