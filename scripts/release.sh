#!/usr/bin/env bash
# release.sh — cross-compile termocode for all supported targets and produce
# tarballs ready to attach to a GitHub release. Mirrors .github/workflows/release.yml.
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

TARGETS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
)

echo "==> termocode release build"
echo "    version : ${VERSION}"
echo "    output  : ${DIST}"
echo

for target in "${TARGETS[@]}"; do
  os="${target%%/*}"
  arch="${target##*/}"
  name="termocode-${VERSION}-${os}-${arch}"
  stage="${DIST}/${name}"
  mkdir -p "${stage}"

  binary="termocode"
  if [[ "${os}" == "windows" ]]; then
    binary="termocode.exe"
  fi

  echo "==> building ${os}/${arch}"
  GOOS="${os}" GOARCH="${arch}" CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w" \
    -o "${stage}/${binary}" ./cmd/termocode

  cp README.md "${stage}/README.md"

  tar -czf "${DIST}/${name}.tar.gz" -C "${DIST}" "${name}"
  rm -rf "${stage}"
done

echo
echo "==> done. artifacts in ${DIST}:"
# Portable size listing (macOS BSD ls vs GNU ls).
if ls -lh --help >/dev/null 2>&1; then
  ls -lh "${DIST}"/*.tar.gz | awk '{printf "    %-10s  %s\n", $5, $NF}'
else
  ls -lh "${DIST}"/*.tar.gz | awk '{printf "    %-10s  %s\n", $5, $NF}'
fi

echo
echo "==> total:"
du -ch "${DIST}"/*.tar.gz | tail -n 1 | awk '{printf "    %s\n", $1}'
