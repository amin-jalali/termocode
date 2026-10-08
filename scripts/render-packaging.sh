#!/usr/bin/env bash
# render-packaging.sh — fill the Homebrew formula and AUR PKGBUILD templates
# in packaging/ with a release version and the tarball checksums.
#
# Usage: scripts/render-packaging.sh <tag> <dist-dir> <out-dir> [owner/repo]
#   tag       release tag, e.g. v0.3.0
#   dist-dir  folder holding termocode-<tag>-<os>-<arch>.tar.gz
#   out-dir   where to write termocode.rb, PKGBUILD and .SRCINFO
#
# Used by the `packaging` job in .github/workflows/release.yml. Also handy
# locally after ./scripts/release.sh to check the rendered files.
set -euo pipefail

if [[ $# -lt 3 ]]; then
  echo "usage: $0 <tag> <dist-dir> <out-dir> [owner/repo]" >&2
  exit 2
fi

TAG="$1"
DIST="$2"
OUT="$3"
REPO="${4:-amin-jalali/termocode}"
VERSION="${TAG#v}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

sha() {
  local f="${DIST}/termocode-${TAG}-$1.tar.gz"
  if [[ ! -f "$f" ]]; then
    echo "missing tarball: $f" >&2
    exit 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$f" | awk '{print $1}'
  else
    shasum -a 256 "$f" | awk '{print $1}'
  fi
}

SHA_LINUX_AMD64="$(sha linux-amd64)"
SHA_LINUX_ARM64="$(sha linux-arm64)"
SHA_DARWIN_AMD64="$(sha darwin-amd64)"
SHA_DARWIN_ARM64="$(sha darwin-arm64)"

render() {
  sed \
    -e "s|@VERSION@|${VERSION}|g" \
    -e "s|@REPO@|${REPO}|g" \
    -e "s|@SHA_LINUX_AMD64@|${SHA_LINUX_AMD64}|g" \
    -e "s|@SHA_LINUX_ARM64@|${SHA_LINUX_ARM64}|g" \
    -e "s|@SHA_DARWIN_AMD64@|${SHA_DARWIN_AMD64}|g" \
    -e "s|@SHA_DARWIN_ARM64@|${SHA_DARWIN_ARM64}|g" \
    "$1" > "$2"
}

mkdir -p "${OUT}"
render "${ROOT}/packaging/homebrew/termocode.rb.tmpl" "${OUT}/termocode.rb"
render "${ROOT}/packaging/aur/PKGBUILD.tmpl" "${OUT}/PKGBUILD"
render "${ROOT}/packaging/aur/SRCINFO.tmpl" "${OUT}/.SRCINFO"

echo "rendered ${OUT}/termocode.rb, ${OUT}/PKGBUILD, ${OUT}/.SRCINFO for ${TAG}"
