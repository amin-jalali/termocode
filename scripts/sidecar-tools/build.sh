#!/bin/sh
# build.sh ARCH LIBC IMAGE [ENGINE]: build termocode's sidecar tools from
# source and put them in the engine volume termocode-tools-ARCH-LIBC
# (plan RC, phase 6), the one the tools sidecar mounts read-only at
# /opt/termocode.
#
#   ARCH   amd64 or arm64 (the container's CPU, as Go spells it)
#   LIBC   glibc or musl (the image's C library; the tools are static,
#          the name only matches what termocode looks for)
#   IMAGE  the image of the container you debug: the volume is filled
#          through a stopped container of it, so no other image is pulled
#   ENGINE docker (default) or a docker with --context: ENGINE="docker --context prod"
#
# Built here: tc-sleep and tc-dap-bridge (this repository) and dlv
# (github.com/go-delve/delve, pinned in tools/sidecar/go.mod), all with
# CGO_ENABLED=0 GOOS=linux. rg comes from fetch-rg.sh when its sha256 is
# pinned; without it termocode searches with grep.
set -eu

arch=${1:?usage: build.sh ARCH LIBC IMAGE [ENGINE]}
libc=${2:?usage: build.sh ARCH LIBC IMAGE [ENGINE]}
image=${3:?usage: build.sh ARCH LIBC IMAGE [ENGINE]}
engine=${4:-docker}
case "$arch" in amd64 | arm64) ;; *) echo "build.sh: ARCH must be amd64 or arm64" >&2; exit 2 ;; esac
case "$libc" in glibc | musl) ;; *) echo "build.sh: LIBC must be glibc or musl" >&2; exit 2 ;; esac

root=$(cd "$(dirname "$0")/../.." && pwd)
vol=termocode-tools-$arch-$libc
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
mkdir -p "$out/bin"

export CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOFLAGS="${GOFLAGS:--p=2}"
(cd "$root" && go build -mod=readonly -trimpath -ldflags=-s -o "$out/bin/tc-sleep" ./cmd/tc-sleep)
(cd "$root" && go build -mod=readonly -trimpath -ldflags=-s -o "$out/bin/tc-dap-bridge" ./cmd/tc-dap-bridge)
(cd "$root/tools/sidecar" && go build -mod=readonly -trimpath -o "$out/bin/dlv" github.com/go-delve/delve/cmd/dlv)
sh "$root/scripts/sidecar-tools/fetch-rg.sh" "$arch" "$out/bin" || echo "build.sh: no rg in the volume; termocode uses grep" >&2

# Fill the volume with docker cp through a stopped container of IMAGE.
$engine volume create "$vol" >/dev/null
fill=termocode-tools-fill-$$
$engine create --name "$fill" -v "$vol:/opt/termocode" --entrypoint /bin/true "$image" >/dev/null
trap '$engine rm -f "$fill" >/dev/null 2>&1; rm -rf "$out"' EXIT
$engine cp "$out/bin" "$fill:/opt/termocode/"
echo "build.sh: $vol has $(ls "$out/bin" | tr '\n' ' ')"
