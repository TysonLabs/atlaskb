#!/bin/sh
# shellcheck disable=SC2059
# install.sh — Cross-platform installer for atlaskb
# Usage: curl -fsSL https://raw.githubusercontent.com/tgeorge06/atlaskb/main/install.sh | sh
#        sh install.sh [--prefix <dir>] [--version <tag>] [--dry-run] [--no-prompt]
set -eu

REPO="tgeorge06/atlaskb"
GITHUB_API="https://api.github.com"
GITHUB_DL="https://github.com"

INSTALL_DIR="${ATLASKB_INSTALL_DIR:-}"
EXPLICIT_PREFIX=false
VERSION=""
DRY_RUN=false
NO_PROMPT=false
NEED_SUDO=false

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	BOLD=$(printf '\033[1m')
	GREEN=$(printf '\033[32m')
	YELLOW=$(printf '\033[33m')
	RED=$(printf '\033[31m')
	CYAN=$(printf '\033[36m')
	RESET=$(printf '\033[0m')
else
	BOLD='' GREEN='' YELLOW='' RED='' CYAN='' RESET=''
fi

info() { printf "${GREEN}info${RESET}  %s\n" "$*"; }
warn() { printf "${YELLOW}warn${RESET}  %s\n" "$*" >&2; }
err() { printf "${RED}error${RESET} %s\n" "$*" >&2; exit 1; }

has_cmd() { command -v "$1" >/dev/null 2>&1; }

maybe_sudo() {
	if [ "$NEED_SUDO" = true ] || { [ ! -w "$INSTALL_DIR" ] && [ "$(id -u)" != "0" ]; }; then
		sudo "$@"
	else
		"$@"
	fi
}

download() {
	url="$1"
	dest="$2"
	if has_cmd curl; then
		curl -fsSL -o "$dest" "$url"
	elif has_cmd wget; then
		wget -qO "$dest" "$url"
	else
		err "Neither curl nor wget found. Install one and retry."
	fi
}

download_stdout() {
	url="$1"
	if has_cmd curl; then
		curl -fsSL "$url"
	elif has_cmd wget; then
		wget -qO- "$url"
	else
		err "Neither curl nor wget found. Install one and retry."
	fi
}

prompt_yn() {
	question="$1"
	default="${2:-n}"
	if [ "$NO_PROMPT" = true ]; then
		[ "$default" = "y" ] && return 0 || return 1
	fi
	if [ "$default" = "y" ]; then
		printf "%s [Y/n] " "$question"
	else
		printf "%s [y/N] " "$question"
	fi
	read -r answer </dev/tty || answer=""
	case "$answer" in
	[Yy]*) return 0 ;;
	[Nn]*) return 1 ;;
	"") [ "$default" = "y" ] && return 0 || return 1 ;;
	*) return 1 ;;
	esac
}

while [ $# -gt 0 ]; do
	case "$1" in
	--prefix)
		[ $# -ge 2 ] || err "--prefix requires a path argument"
		INSTALL_DIR="$2"
		EXPLICIT_PREFIX=true
		shift 2
		;;
	--version)
		[ $# -ge 2 ] || err "--version requires a version argument"
		VERSION="$2"
		shift 2
		;;
	--dry-run)
		DRY_RUN=true
		shift
		;;
	--no-prompt)
		NO_PROMPT=true
		shift
		;;
	-h | --help)
		cat <<'USAGE'
atlaskb installer

Usage:
  curl -fsSL https://raw.githubusercontent.com/tgeorge06/atlaskb/main/install.sh | sh
  sh install.sh [OPTIONS]

Options:
  --prefix <path>   Install directory (default: /usr/local/bin, or $ATLASKB_INSTALL_DIR)
  --version <tag>   Install a specific version (e.g. v0.1.0; default: latest)
  --dry-run         Print actions without executing
  --no-prompt       Non-interactive mode (for CI)
  -h, --help        Show this help
USAGE
		exit 0
		;;
	*)
		err "Unknown option: $1 (see --help)"
		;;
	esac
done

resolve_install_dir() {
	if [ "$EXPLICIT_PREFIX" = true ] || [ -n "$INSTALL_DIR" ]; then
		return
	fi

	if [ -w /usr/local/bin ] 2>/dev/null; then
		INSTALL_DIR="/usr/local/bin"
	elif [ "$(id -u)" = "0" ]; then
		INSTALL_DIR="/usr/local/bin"
	else
		if has_cmd sudo && sudo -n true 2>/dev/null; then
			INSTALL_DIR="/usr/local/bin"
		elif has_cmd sudo; then
			if prompt_yn "Install to /usr/local/bin (requires sudo)?"; then
				INSTALL_DIR="/usr/local/bin"
				NEED_SUDO=true
			else
				INSTALL_DIR="${HOME}/.local/bin"
			fi
		else
			INSTALL_DIR="${HOME}/.local/bin"
		fi
	fi

	info "Install directory: ${BOLD}${INSTALL_DIR}${RESET}"
}

detect_platform() {
	OS="$(uname -s)"
	ARCH="$(uname -m)"

	case "$OS" in
	Linux) PLATFORM="linux" ;;
	Darwin) PLATFORM="macos" ;;
	MINGW* | MSYS* | CYGWIN* | Windows_NT)
		err "Windows detected. Use Scoop instead:
  scoop bucket add atlaskb https://github.com/tgeorge06/scoop-atlaskb
  scoop install atlaskb"
		;;
	*) err "Unsupported OS: $OS" ;;
	esac

	case "$ARCH" in
	x86_64 | amd64) ARCH="x86_64" ;;
	aarch64 | arm64) ARCH="aarch64" ;;
	*) err "Unsupported architecture: $ARCH" ;;
	esac

	TARBALL="atlaskb-${PLATFORM}-${ARCH}.tar.gz"
}

resolve_version() {
	if [ -n "$VERSION" ]; then
		case "$VERSION" in
		v*) ;;
		*) VERSION="v${VERSION}" ;;
		esac
		info "Using specified version: ${VERSION}"
		return
	fi

	info "Fetching latest release..."
	LATEST_JSON="$(download_stdout "${GITHUB_API}/repos/${REPO}/releases/latest")"
	VERSION="$(printf '%s' "$LATEST_JSON" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')"
	[ -n "$VERSION" ] || err "Failed to determine latest version from GitHub API"
	info "Latest version: ${BOLD}${VERSION}${RESET}"
}

verify_checksum() {
	tarball_path="$1"
	checksums_path="$2"

	expected="$(grep "$(basename "$tarball_path")" "$checksums_path" | awk '{print $1}')"
	[ -n "$expected" ] || {
		warn "No checksum found for $(basename "$tarball_path"), skipping verification"
		return 0
	}

	if has_cmd sha256sum; then
		actual="$(sha256sum "$tarball_path" | awk '{print $1}')"
	elif has_cmd shasum; then
		actual="$(shasum -a 256 "$tarball_path" | awk '{print $1}')"
	else
		warn "No sha256sum or shasum found, skipping checksum verification"
		return 0
	fi

	[ "$expected" = "$actual" ] || err "Checksum mismatch!
  Expected: ${expected}
  Got:      ${actual}"
	info "Checksum verified"
}

do_install() {
	DOWNLOAD_URL="${GITHUB_DL}/${REPO}/releases/download/${VERSION}/${TARBALL}"
	CHECKSUMS_URL="${GITHUB_DL}/${REPO}/releases/download/${VERSION}/checksums.sha256"

	if [ "$DRY_RUN" = true ]; then
		printf "\n${BOLD}Dry run — would perform:${RESET}\n"
		printf "  1. Download  %s\n" "$DOWNLOAD_URL"
		printf "  2. Download  %s\n" "$CHECKSUMS_URL"
		printf "  3. Verify    SHA256 checksum\n"
		printf "  4. Extract   atlaskb to %s/atlaskb\n" "$INSTALL_DIR"
		printf "  5. Health    atlaskb version\n"
		return
	fi

	TMPDIR_PATH="$(mktemp -d)"
	trap 'rm -rf "$TMPDIR_PATH"' EXIT

	info "Downloading ${TARBALL}..."
	download "$DOWNLOAD_URL" "${TMPDIR_PATH}/${TARBALL}"

	info "Downloading checksums..."
	if download "${CHECKSUMS_URL}" "${TMPDIR_PATH}/checksums.sha256" 2>/dev/null; then
		verify_checksum "${TMPDIR_PATH}/${TARBALL}" "${TMPDIR_PATH}/checksums.sha256"
	else
		warn "checksums.sha256 not found in release, skipping verification"
	fi

	info "Extracting..."
	tar xzf "${TMPDIR_PATH}/${TARBALL}" -C "${TMPDIR_PATH}"

	BIN_PATH="$(find "${TMPDIR_PATH}" -type f -name atlaskb | head -1 || true)"
	[ -n "$BIN_PATH" ] || err "Could not find atlaskb binary in archive"

	maybe_sudo mkdir -p "$INSTALL_DIR"
	maybe_sudo install -m 0755 "$BIN_PATH" "${INSTALL_DIR}/atlaskb"
	info "Installed to ${BOLD}${INSTALL_DIR}/atlaskb${RESET}"
}

ensure_path() {
	case ":$PATH:" in
	*":$INSTALL_DIR:"*) return ;;
	esac

	warn "$INSTALL_DIR is not in your PATH."
	case "$SHELL" in
	*/zsh)
		RC_FILE="${HOME}/.zshrc"
		;;
	*/bash)
		RC_FILE="${HOME}/.bashrc"
		;;
	*)
		RC_FILE=""
		;;
	esac

	if [ -n "$RC_FILE" ]; then
		if prompt_yn "Add ${INSTALL_DIR} to PATH in ${RC_FILE}?" "y"; then
			printf '\n# Added by atlaskb installer\nexport PATH="%s:$PATH"\n' "$INSTALL_DIR" >>"$RC_FILE"
			info "Appended PATH update to ${RC_FILE} (open a new shell)."
		fi
	fi

	printf "Current shell usage:\n  export PATH=\"%s:\$PATH\"\n" "$INSTALL_DIR"
}

health_check() {
	[ "$DRY_RUN" = true ] && return
	if "${INSTALL_DIR}/atlaskb" version >/dev/null 2>&1; then
		info "Verified: $("${INSTALL_DIR}/atlaskb" version | head -1)"
	else
		warn "Binary installed but version check failed. Try: ${INSTALL_DIR}/atlaskb version"
	fi
}

printf "\n  atlaskb installer\n\n"
detect_platform
resolve_install_dir
resolve_version
do_install
ensure_path
health_check

if [ "$DRY_RUN" = true ]; then
	printf "\n  Dry run complete for atlaskb %s.\n\n" "${VERSION}"
else
	printf "\n  atlaskb %s installed successfully!\n" "${VERSION}"
	printf "  Run ${CYAN}atlaskb setup${RESET} then ${CYAN}atlaskb${RESET} to get started.\n\n"
fi
