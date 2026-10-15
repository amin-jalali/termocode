#!/usr/bin/env bash
# release.sh — local smoke build of all release tarballs into dist/, with
# checksums.txt. Real releases come from .github/workflows/release.yml.
#
# The host target is built with cgo, like the CI release. The other
# targets need a C cross-compiler for cgo, so here they are built with
# CGO_ENABLED=0: they work, but the system clipboard falls back to
# wl-copy / xclip / pbcopy (copy only). Do not publish those.
# android/arm64 uses the Android NDK clang when ANDROID_NDK_HOME is set
# (like the CI release); without it, it is built with CGO_ENABLED=0 and
# cannot resolve host names on a phone (no /etc/resolv.conf).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

# Resolve a version string. Prefer git describe, fall back to "dev".
if command -v git >/dev/null 2>&1 && git rev-parse --git-dir >/dev/null 2>&1; then
  VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo "dev")"
else
  VERSION="dev"
fi

DIST="${ROOT}/dist"
rm -rf "${DIST}"
mkdir -p "${DIST}"

host="$(go env GOOS)/$(go env GOARCH)"
echo "==> termocode release build ${VERSION} -> ${DIST}"
ndk_cc="${ANDROID_NDK_HOME:-}/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android24-clang"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 android/arm64; do
  cgo=0
  cc="${CC:-}"
  [[ "${target}" == "${host}" ]] && cgo=1
  if [[ "${target}" == android/arm64 && -n "${ANDROID_NDK_HOME:-}" && -x "${ndk_cc}" ]]; then
    cgo=1
    cc="${ndk_cc}"
  fi
  CC="${cc}" CGO_ENABLED="${cgo}" ./scripts/build-release.sh "${VERSION}" "${target%%/*}" "${target##*/}" "${DIST}"
done

(cd "${DIST}" && if command -v sha256sum >/dev/null; then sha256sum ./*.tar.gz; else shasum -a 256 ./*.tar.gz; fi |
  sed 's# \./# #' > checksums.txt)

echo
echo "==> done:"
ls -lh "${DIST}"
