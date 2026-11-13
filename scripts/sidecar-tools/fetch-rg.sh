#!/bin/sh
# fetch-rg.sh ARCH OUTDIR: put one pinned ripgrep release (a static Linux
# build) at OUTDIR/rg for the tools sidecar (plan RC, phase 6). ripgrep is
# a Rust program, so termocode does not build it; this downloads it and
# installs it only when its sha256 matches the hash pinned below.
#
# Owner steps (once per new RG_VERSION): download the two archives from
# the official release page, check them against the release's own
# .sha256 files, and write the hashes into RG_SHA256_AMD64 and
# RG_SHA256_ARM64. Until then this script refuses, and termocode searches
# a container with grep (or reads the files) instead.
set -eu

RG_VERSION=14.1.1
RG_SHA256_AMD64="" # ripgrep-$RG_VERSION-x86_64-unknown-linux-musl.tar.gz
RG_SHA256_ARM64="" # ripgrep-$RG_VERSION-aarch64-unknown-linux-gnu.tar.gz

arch=${1:?usage: fetch-rg.sh ARCH OUTDIR}
out=${2:?usage: fetch-rg.sh ARCH OUTDIR}

case "$arch" in
amd64) triple=x86_64-unknown-linux-musl; sum=$RG_SHA256_AMD64 ;;
arm64) triple=aarch64-unknown-linux-gnu; sum=$RG_SHA256_ARM64 ;;
*) echo "fetch-rg.sh: no pinned sha256 for $arch; termocode uses grep there" >&2; exit 2 ;;
esac
if [ -z "$sum" ]; then
	echo "fetch-rg.sh: no pinned sha256 for ripgrep $RG_VERSION $triple yet (see Owner steps at the top); skipping rg" >&2
	exit 2
fi

name=ripgrep-$RG_VERSION-$triple
url=https://github.com/BurntSushi/ripgrep/releases/download/$RG_VERSION/$name.tar.gz
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL --proto '=https' -o "$tmp/rg.tgz" "$url"
echo "$sum  $tmp/rg.tgz" | sha256sum -c - >/dev/null || {
	echo "fetch-rg.sh: sha256 of $name does not match the pinned hash; nothing installed" >&2
	exit 1
}
tar -xzf "$tmp/rg.tgz" -C "$tmp" "$name/rg"
mkdir -p "$out"
install -m 0755 "$tmp/$name/rg" "$out/rg"
echo "rg $RG_VERSION -> $out/rg"
