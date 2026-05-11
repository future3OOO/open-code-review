#!/usr/bin/env bash
#
# release.sh — Build all platform binaries and publish to internal-release repo
#
# Usage:
#   ./scripts/release.sh              # Builds using latest git tag as version
#   ./scripts/release.sh v0.1.0       # Explicit version tag
#
# Prerequisites:
#   - Git tag exists for the version (or pass version explicitly)
#   - Go 1.25+ installed
#   - ~/internal-release repo exists with a writable bin/ directory
#
# Environment variables:
#   OCR_INTERNAL_RELEASE   Override the default path to internal-release repo
#                          (default: $HOME/internal-release)
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

info()  { echo "[INFO]  $*"; }
warn()  { echo "[WARN]  $*"; }
error() { echo "[ERROR] $*"; }
die()   { error "$*"; exit 1; }

VERSION="${1:-}"

if [ -z "$VERSION" ]; then
    VERSION=$(git -C "$PROJECT_ROOT" describe --tags --abbrev=0 2>/dev/null || true)
    if [ -z "$VERSION" ]; then
        die "No git tag found. Pass a version explicitly or create a tag first."
    fi
    info "Using latest git tag: ${VERSION}"
else
    [[ "$VERSION" != v* ]] && VERSION="v${VERSION}"
fi

info "=== OpenCodeReview Release: ${VERSION} ==="

# ── Pre-flight checks ────────────────────────────────────────────────────────
command -v go >/dev/null 2>&1 || die "'go' is required but not installed."

cd "$PROJECT_ROOT"
if [ -n "$(git status --porcelain)" ]; then
    warn "Working tree has uncommitted changes. Proceeding anyway..."
fi

# ── Build all platforms ──────────────────────────────────────────────────────
DIST_DIR="${PROJECT_ROOT}/dist"
mkdir -p "$DIST_DIR"
rm -f "${DIST_DIR}/opencodereview-${VERSION}-"*

GIT_COMMIT="$(git rev-parse --short HEAD)"
BUILD_DATE="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
LD_FLAGS="-s -w -X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildDate=${BUILD_DATE}"

TARGETS=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
)

for pair in "${TARGETS[@]}"; do
    GOOS="${pair%/*}"
    GOARCH="${pair#*/}"
    OS_ARCH="${GOOS}-${GOARCH}"
    OUTPUT_NAME="opencodereview-${VERSION}-${OS_ARCH}"

    info "Building ${GOOS}/${GOARCH} → ${OUTPUT_NAME}"
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
        go build -ldflags "${LD_FLAGS}" \
        -o "${DIST_DIR}/${OUTPUT_NAME}" \
        ./cmd/opencodereview
done

# ── Publish to internal release repo ────────────────────────────────────────
INTERNAL_ROOT="${OCR_INTERNAL_RELEASE:-$HOME/internal-release}"
[ -d "$INTERNAL_ROOT" ] || die "internal-release repo not found at ${INTERNAL_ROOT}"

BIN_DIR="${INTERNAL_ROOT}/bin"
VERSION_DIR="${BIN_DIR}/${VERSION}"

info ""
info "Publishing to ${INTERNAL_ROOT} ..."

# Create versioned directory, clean old files if re-publishing same version
mkdir -p "$VERSION_DIR"
rm -f "$VERSION_DIR"/*

for f in "${DIST_DIR}"/opencodereview-"${VERSION}"-*; do
    SRC_BASENAME="$(basename "$f")"
    # Strip version prefix to get canonical name: opencodereview-v0.1.0-darwin-arm64 → opencodereview-darwin-arm64
    TARGET_NAME="${SRC_BASENAME/opencodereview-${VERSION}-/opencodereview-}"
    info "  ${TARGET_NAME}"
    cp "$f" "$VERSION_DIR/${TARGET_NAME}"
done

# Generate checksum using final filenames (without version in filename)
(cd "$VERSION_DIR" && shasum -a 256 opencodereview-* | sort > sha256sum.txt)
info "  sha256sum.txt written"

# Append version to VERSION file (last line = latest)
printf "%s\n" "$VERSION" >> "${INTERNAL_ROOT}/VERSION"
info "  VERSION entry appended"

# ── Commit and push ──────────────────────────────────────────────────────────
cd "$INTERNAL_ROOT"

git add -A

if [ -n "$(git status --porcelain)" ]; then
    git commit -m "release: opencodereview ${VERSION}"
    git push
    info ""
    info "=== Release ${VERSION} published ==="
else
    info "No new changes to commit — binaries unchanged."
fi
