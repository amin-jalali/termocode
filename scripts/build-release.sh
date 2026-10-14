#!/usr/bin/env bash
# build-release.sh — build one release target and pack it as
#   <dist>/termocode-<version>-<os>-<arch>.tar.gz
# holding termocode-<version>-<os>-<arch>/{termocode,README.md,LICENSE}.
#
# Usage: scripts/build-release.sh <version> <os> <arch> [dist-dir]
#
# The version is linked in (`termocode --version`). CGO_ENABLED and CC
# come from the environment, so the caller picks the toolchain. Release
# builds use cgo (the system clipboard library needs it); see the build
# matrix in .github/workflows/release.yml.
set -euo pipefail

if [[ $# -lt 3 ]]; then
  echo "usage: $0 <version> <os> <arch> [dist-dir]" >&2
  exit 2
fi
version="$1"
os="$2"
arch="$3"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist="${4:-${ROOT}/dist}"

name="termocode-${version}-${os}-${arch}"
stage="${dist}/${name}"
rm -rf "${stage}"
mkdir -p "${stage}"

echo "==> building ${name} (CGO_ENABLED=${CGO_ENABLED:-default} CC=${CC:-default})"
(
  cd "${ROOT}"
  GOOS="${os}" GOARCH="${arch}" \
    go build -trimpath \
    -ldflags="-s -w -X main.version=${version}" \
    -o "${stage}/termocode" ./cmd/termocode
)
cp "${ROOT}/README.md" "${ROOT}/LICENSE" "${stage}/"

tar -czf "${dist}/${name}.tar.gz" -C "${dist}" "${name}"
rm -rf "${stage}"
echo "    ${dist}/${name}.tar.gz"
