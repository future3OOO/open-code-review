#!/usr/bin/env bash
# copy-to-internal-repo.sh — Upload artifacts to internal-release repo via temp clone.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_common.sh"

PROJECT_ROOT="$(resolve_project_root)"

VERSION_TAG="${VERSION_TAG:?VERSION_TAG is required}"
NPM_VERSION="${NPM_VERSION:-$(npm_version_from_tag "$VERSION_TAG")}"

DIST_DIR="${PROJECT_ROOT}/dist"

# ── Temp clone lifecycle ─────────────────────────────────────────────────────
TMP_REPO=""

cleanup_tmp_repo() {
    if [ -n "$TMP_REPO" ] && [ -d "$TMP_REPO" ]; then
        rm -rf "$TMP_REPO"
        info "Cleaned up temporary clone."
    fi
}
trap cleanup_tmp_repo EXIT INT TERM

setup_temp_clone() {
    TMP_REPO=$(mktemp -d -t "ocr-internal-repo.XXXXXX")
    info "Cloning internal-release repo (sparse) to temp directory..."
    git clone --depth 1 --filter=blob:none --sparse \
        git@gitlab.alibaba-inc.com:open-code-review/internal-release.git "$TMP_REPO"
    cd "$TMP_REPO"
    git sparse-checkout set --no-cone VERSION
    cd "$PROJECT_ROOT"
    success "Temp clone ready at ${TMP_REPO}"
}

# ── Paths set by setup_temp_clone after TMP_REPO is available ───────────────
BIN_DIR=""
VERSION_BIN_DIR=""

copy_artifacts() {
    mkdir -p "$VERSION_BIN_DIR"

    local count=0
    for f in "$DIST_DIR"/opencodereview-${VERSION_TAG}-*; do
        [ -f "$f" ] || continue
        local src_base
        src_base=$(basename "$f")
        # Strip version prefix: opencodereview-v1.0-darwin-arm64 → opencodereview-darwin-arm64
        local target_name="${src_base/opencodereview-${VERSION_TAG}-/opencodereview-}"
        info "  Copying ${target_name}"
        cp "$f" "$VERSION_BIN_DIR/${target_name}"
        count=$((count + 1))
    done

    if [ "$count" -eq 0 ]; then
        die "No binaries found in ${DIST_DIR} matching pattern opencodereview-${VERSION_TAG}-*"
    fi

    success "Copied ${count} binaries"
}

generate_checksums() {
    (cd "$VERSION_BIN_DIR" && shasum -a 256 opencodereview-* | sort > sha256sum.txt)
    success "sha256sum.txt generated"
}

update_version_file() {
    local version_file="${TMP_REPO}/VERSION"
    # Only add version if it's not already the last line
    if [ -f "$version_file" ]; then
        local last_line
        last_line=$(tail -1 "$version_file" 2>/dev/null || true)
        if [ "$last_line" = "$VERSION_TAG" ]; then
            info "VERSION file already ends with ${VERSION_TAG}, skipping."
            return 0
        fi
    fi
    printf "%s\n" "$VERSION_TAG" >> "$version_file"
    info "VERSION file updated (added ${VERSION_TAG})"
}

commit_and_push() {
    cd "$TMP_REPO"

    git add --sparse -A

    if git diff --cached --quiet; then
        info "No changes to commit — binaries already match."
        return 0
    fi

    info "Committing to internal-release repo..."
    git config user.name "open-code-review-bot"
    git config user.email "bot@open-code-review.local"
    git commit -m "release: opencodereview ${VERSION_TAG} (npm ${NPM_VERSION})"
    git push origin master 2>/dev/null || git push
    success "Pushed to internal-release repo"
}

# ── Execution ────────────────────────────────────────────────────────────────
info "=== Publishing artifacts to internal-release repo ==="
info "Version: ${VERSION_TAG}"
echo ""

setup_temp_clone

# Set paths now TMP_REPO is available
BIN_DIR="${TMP_REPO}/bin"
VERSION_BIN_DIR="${BIN_DIR}/${VERSION_TAG}"

copy_artifacts
generate_checksums
update_version_file
commit_and_push

echo ""
success "Artifacts published successfully"
