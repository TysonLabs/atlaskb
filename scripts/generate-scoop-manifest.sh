#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
Generate a Scoop manifest for AtlasKB.

Usage:
  scripts/generate-scoop-manifest.sh --version 0.1.0 --hash <sha256> [options]

Options:
  --version <version>    Version without leading v (required)
  --hash <sha256>        SHA256 hash for atlaskb-windows-x86_64.tar.gz (required)
  --repo <owner/repo>    Source repository (default: TysonLabs/atlaskb)
  --output <path>        Output file (default: stdout)
  -h, --help             Show help
EOF
}

VERSION=""
HASH=""
REPO="TysonLabs/atlaskb"
OUTPUT=""

while [[ $# -gt 0 ]]; do
	case "$1" in
	--version)
		VERSION="${2:-}"
		shift 2
		;;
	--hash)
		HASH="${2:-}"
		shift 2
		;;
	--repo)
		REPO="${2:-}"
		shift 2
		;;
	--output)
		OUTPUT="${2:-}"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "error: unknown option: $1" >&2
		usage >&2
		exit 1
		;;
	esac
done

if [[ -z "$VERSION" || -z "$HASH" ]]; then
	echo "error: --version and --hash are required" >&2
	usage >&2
	exit 1
fi

CONTENT="$(cat <<EOF
{
  "version": "${VERSION}",
  "description": "Organizational code knowledge base CLI",
  "homepage": "https://github.com/${REPO}",
  "license": "Proprietary",
  "architecture": {
    "64bit": {
      "url": "https://github.com/${REPO}/releases/download/v${VERSION}/atlaskb-windows-x86_64.tar.gz",
      "hash": "${HASH}"
    }
  },
  "bin": "atlaskb.exe",
  "checkver": "github",
  "autoupdate": {
    "architecture": {
      "64bit": {
        "url": "https://github.com/${REPO}/releases/download/v\$version/atlaskb-windows-x86_64.tar.gz"
      }
    }
  }
}
EOF
)"

if [[ -n "$OUTPUT" ]]; then
	mkdir -p "$(dirname "$OUTPUT")"
	printf '%s\n' "$CONTENT" >"$OUTPUT"
else
	printf '%s\n' "$CONTENT"
fi
