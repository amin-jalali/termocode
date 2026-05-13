#!/usr/bin/env bash
# dev.sh — quick developer loop: vet, test, build.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

header() {
  printf '\n\033[1;36m==> %s\033[0m\n' "$1"
}

header "go vet ./..."
go vet ./...

header "go test ./..."
go test ./...

header "go build -o termocode ./cmd/termocode"
go build -o termocode ./cmd/termocode

header "done"
echo "    binary: ${ROOT}/termocode"
