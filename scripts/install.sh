#!/bin/sh
# install.sh — install or upgrade termocode from a GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | sh
#
# Options (pass after `sh -s --` when piping):
#   --version vX.Y.Z   install this version (default: the latest release)
#   --force            reinstall even when this version is already installed
#   --uninstall        remove the installed binary
#   -h, --help         show this help
#
# Environment:
#   TERMOCODE_VERSION      same as --version
#   PREFIX                 install to $PREFIX/bin (default: $HOME/.local)
#   TERMOCODE_RELEASE_URL  base URL of the releases (default:
#                          https://github.com/amin-jalali/termocode/releases);
#                          used by the tests with a local server
#
# The script never uses sudo and never installs system packages. It
# downloads the tarball for this OS and CPU, checks its sha256 against
# checksums.txt, and copies the binary into place.
set -eu

REPO="amin-jalali/termocode"
BASE_URL="${TERMOCODE_RELEASE_URL:-https://github.com/${REPO}/releases}"
PREFIX="${PREFIX:-${HOME}/.local}"
BIN_DIR="${PREFIX}/bin"
WANT_VERSION="${TERMOCODE_VERSION:-}"
NVIM_MIN_MAJOR=0
NVIM_MIN_MINOR=10
FORCE=0
ACTION=install
TMP_DIR=""

say() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Install or upgrade termocode from a GitHub release.

  curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | sh
  curl -fsSL .../install.sh | sh -s -- --version v0.3.0

Options:
  --version vX.Y.Z   install this version (default: the latest release)
  --force            reinstall even when this version is already installed
  --uninstall        remove the installed binary
  -h, --help         show this help

Environment:
  TERMOCODE_VERSION  same as --version
  PREFIX             install to $PREFIX/bin (default: $HOME/.local)
EOF
}

cleanup() {
	if [ -n "${TMP_DIR}" ] && [ -d "${TMP_DIR}" ]; then
		rm -rf "${TMP_DIR}"
	fi
}

has() { command -v "$1" >/dev/null 2>&1; }

# fetch URL FILE downloads URL into FILE.
fetch() {
	if has curl; then
		curl -fsSL --retry 2 -o "$2" "$1"
	elif has wget; then
		wget -q -O "$2" "$1"
	else
		die "need curl or wget to download termocode"
	fi
}

# latest_tag prints the newest release tag. It follows the
# releases/latest redirect, so no GitHub API (and no rate limit).
latest_tag() {
	if has curl; then
		url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "${BASE_URL}/latest") || return 1
	elif has wget; then
		url=$(wget -q -S -O /dev/null "${BASE_URL}/latest" 2>&1 |
			sed -n 's/^ *[Ll]ocation: *//p' | tail -n 1 | tr -d '\r') || return 1
	else
		die "need curl or wget to download termocode"
	fi
	tag=${url##*/}
	case "${tag}" in
	v[0-9]*) printf '%s\n' "${tag}" ;;
	*) return 1 ;;
	esac
}

detect_os() {
	case "$(uname -s)" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	*) die "unsupported OS: $(uname -s) (termocode runs on Linux and macOS)" ;;
	esac
}

detect_arch() {
	arch=$(uname -m)
	case "${arch}" in
	x86_64 | amd64)
		# An x86 shell under Rosetta on Apple Silicon: use the native build.
		if [ "$(uname -s)" = Darwin ] &&
			[ "$(sysctl -n hw.optional.arm64 2>/dev/null || echo 0)" = 1 ]; then
			echo arm64
		else
			echo amd64
		fi
		;;
	aarch64 | arm64) echo arm64 ;;
	*) die "unsupported CPU: ${arch} (release builds exist for amd64 and arm64)" ;;
	esac
}

# sha256_of FILE prints the file's sha256.
sha256_of() {
	if has sha256sum; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif has shasum; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	elif has openssl; then
		openssl dgst -sha256 "$1" | sed 's/^.*= *//'
	else
		die "need sha256sum, shasum or openssl to check the download"
	fi
}

# installed_version prints the version of the installed binary, if any.
installed_version() {
	if [ -x "${BIN_DIR}/termocode" ]; then
		"${BIN_DIR}/termocode" --version 2>/dev/null | sed -n 's/^termocode //p' | head -n 1
	fi
}

check_path() {
	case ":${PATH}:" in
	*":${BIN_DIR}:"*) return 0 ;;
	esac
	say ""
	say "${BIN_DIR} is not on your PATH. Add it:"
	case "${SHELL:-}" in
	*/fish) say "  fish_add_path ${BIN_DIR}" ;;
	*/zsh) say "  echo 'export PATH=\"${BIN_DIR}:\$PATH\"' >> ~/.zshrc" ;;
	*) say "  echo 'export PATH=\"${BIN_DIR}:\$PATH\"' >> ~/.bashrc" ;;
	esac
	say "Then open a new terminal."
}

nvim_hint() {
	if [ "$(uname -s)" = Darwin ]; then
		say "  brew install neovim"
		return
	fi
	if has pacman; then
		say "  sudo pacman -S neovim"
	elif has dnf; then
		say "  sudo dnf install neovim"
	elif has zypper; then
		say "  sudo zypper install neovim"
	elif has apk; then
		say "  sudo apk add neovim"
	elif has apt-get; then
		say "  Debian/Ubuntu often ship an old nvim. Get a current one from"
		say "  https://github.com/neovim/neovim/releases (nvim-linux-*.tar.gz),"
		say "  or: brew install neovim"
	else
		say "  https://github.com/neovim/neovim/releases"
	fi
}

check_nvim() {
	if ! has nvim; then
		say ""
		say "termocode needs Neovim ${NVIM_MIN_MAJOR}.${NVIM_MIN_MINOR} or newer, and nvim was not found. Install it:"
		nvim_hint
		return 0
	fi
	ver=$(nvim --version 2>/dev/null | sed -n '1s/^NVIM v\([0-9][0-9]*\.[0-9][0-9]*\).*/\1/p')
	major=${ver%%.*}
	minor=${ver#*.}
	if [ -z "${ver}" ]; then
		warn "could not read the nvim version; termocode needs ${NVIM_MIN_MAJOR}.${NVIM_MIN_MINOR} or newer"
		return 0
	fi
	if [ "${major}" -gt "${NVIM_MIN_MAJOR}" ] ||
		{ [ "${major}" -eq "${NVIM_MIN_MAJOR}" ] && [ "${minor}" -ge "${NVIM_MIN_MINOR}" ]; }; then
		say "Found nvim ${ver}."
		return 0
	fi
	say ""
	say "Your nvim is ${ver}; termocode needs ${NVIM_MIN_MAJOR}.${NVIM_MIN_MINOR} or newer. Upgrade it:"
	nvim_hint
}

do_uninstall() {
	if [ -e "${BIN_DIR}/termocode" ]; then
		rm -f "${BIN_DIR}/termocode"
		say "Removed ${BIN_DIR}/termocode."
	else
		say "termocode is not installed in ${BIN_DIR}."
	fi
	say "Your settings in ~/.config/termocode were kept."
}

do_install() {
	os=$(detect_os)
	arch=$(detect_arch)

	tag="${WANT_VERSION}"
	if [ -z "${tag}" ]; then
		tag=$(latest_tag) || die "could not find the latest release at ${BASE_URL}/latest"
	fi
	case "${tag}" in
	v*) ;;
	*) tag="v${tag}" ;;
	esac

	current=$(installed_version || true)
	if [ "${FORCE}" = 0 ] && [ "${current}" = "${tag}" ]; then
		say "termocode ${tag} is already installed in ${BIN_DIR}."
		check_path
		check_nvim
		return 0
	fi

	name="termocode-${tag}-${os}-${arch}"
	tarball="${name}.tar.gz"
	say "Downloading termocode ${tag} (${os}/${arch})..."

	TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t termocode)
	fetch "${BASE_URL}/download/${tag}/${tarball}" "${TMP_DIR}/${tarball}" ||
		die "download failed: ${BASE_URL}/download/${tag}/${tarball}"
	fetch "${BASE_URL}/download/${tag}/checksums.txt" "${TMP_DIR}/checksums.txt" ||
		die "download failed: ${BASE_URL}/download/${tag}/checksums.txt"

	want=$(awk -v f="${tarball}" '$2 == f || $2 == "*" f { print $1; exit }' "${TMP_DIR}/checksums.txt")
	[ -n "${want}" ] || die "${tarball} is not listed in checksums.txt"
	got=$(sha256_of "${TMP_DIR}/${tarball}")
	[ "${got}" = "${want}" ] || die "checksum mismatch for ${tarball} (want ${want}, got ${got}); nothing was installed"

	tar -xzf "${TMP_DIR}/${tarball}" -C "${TMP_DIR}" || die "could not unpack ${tarball}"
	[ -f "${TMP_DIR}/${name}/termocode" ] || die "${tarball} has no ${name}/termocode"

	mkdir -p "${BIN_DIR}" || die "could not create ${BIN_DIR}"
	# Copy next to the target, then rename: the swap is atomic and works
	# while an old termocode is running.
	cp "${TMP_DIR}/${name}/termocode" "${BIN_DIR}/.termocode.new" ||
		die "could not write to ${BIN_DIR}"
	chmod 755 "${BIN_DIR}/.termocode.new"
	mv -f "${BIN_DIR}/.termocode.new" "${BIN_DIR}/termocode"

	if [ -n "${current}" ]; then
		say "Upgraded termocode ${current} -> ${tag} in ${BIN_DIR}."
	else
		say "Installed termocode ${tag} in ${BIN_DIR}."
	fi
	check_path
	check_nvim
	say ""
	say "Next: run 'termocode setup' to check your system (on Linux it can install a Nerd Font)."
	say "Then: cd into a project and run 'termocode'."
}

main() {
	while [ $# -gt 0 ]; do
		case "$1" in
		--version)
			[ $# -ge 2 ] || die "--version needs a value, e.g. --version v0.3.0"
			WANT_VERSION="$2"
			shift 2
			;;
		--version=*)
			WANT_VERSION="${1#--version=}"
			shift
			;;
		--force)
			FORCE=1
			shift
			;;
		--uninstall)
			ACTION=uninstall
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*) die "unknown option: $1 (see --help)" ;;
		esac
	done

	trap cleanup EXIT
	trap 'cleanup; exit 1' INT TERM

	case "${ACTION}" in
	uninstall) do_uninstall ;;
	*) do_install ;;
	esac
}

main "$@"
