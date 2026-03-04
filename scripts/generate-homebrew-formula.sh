#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'EOF'
Generate a Homebrew formula for Atlaskb in a private tap.

Usage:
  scripts/generate-homebrew-formula.sh --tag v0.1.0 [options]

Options:
  --tag <tag>               Git tag to package (required, e.g. v0.1.0)
  --revision <sha>          Commit SHA for the tag (default: resolve from git)
  --source-repo <url>       Git URL for source (default: origin remote)
  --homepage <url>          Formula homepage URL
  --desc <text>             Formula description text
  --output <path>           Write formula to file (default: stdout)
  -h, --help                Show this help text

Notes:
  - For private GitHub repos, prefer an SSH URL such as:
      ssh://git@github.com/<owner>/<repo>.git
  - The generated formula builds from source with Homebrew's Go toolchain.
EOF
}

normalize_repo_url() {
	local url="$1"

	if [[ "$url" =~ ^git@github\.com:(.+)$ ]]; then
		printf 'ssh://git@github.com/%s\n' "${BASH_REMATCH[1]}"
		return 0
	fi

	if [[ "$url" =~ ^https://github\.com/.+[^/]$ ]] && [[ ! "$url" =~ \.git$ ]]; then
		printf '%s.git\n' "$url"
		return 0
	fi

	printf '%s\n' "$url"
}

TAG=""
REVISION=""
SOURCE_REPO=""
HOMEPAGE="https://github.com/tgeorge06/atlaskb"
DESC="Organizational code knowledge base CLI"
OUTPUT=""

while [[ $# -gt 0 ]]; do
	case "$1" in
		--tag)
			TAG="${2:-}"
			shift 2
			;;
		--revision)
			REVISION="${2:-}"
			shift 2
			;;
		--source-repo)
			SOURCE_REPO="${2:-}"
			shift 2
			;;
		--homepage)
			HOMEPAGE="${2:-}"
			shift 2
			;;
		--desc)
			DESC="${2:-}"
			shift 2
			;;
		--output)
			OUTPUT="${2:-}"
			shift 2
			;;
		-h|--help)
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

if [[ -z "$TAG" ]]; then
	echo "error: --tag is required" >&2
	usage >&2
	exit 1
fi

if [[ -z "$REVISION" ]]; then
	if ! REVISION="$(git rev-list -n 1 "$TAG" 2>/dev/null)"; then
		echo "error: could not resolve revision for tag '$TAG'" >&2
		exit 1
	fi
fi

if [[ -z "$SOURCE_REPO" ]]; then
	if ! SOURCE_REPO="$(git remote get-url origin 2>/dev/null)"; then
		echo "error: could not determine source repo from git remote 'origin'" >&2
		echo "       pass --source-repo explicitly" >&2
		exit 1
	fi
fi

SOURCE_REPO="$(normalize_repo_url "$SOURCE_REPO")"

if [[ ! "$SOURCE_REPO" =~ \.git$ ]]; then
	echo "error: source repo URL must end in .git for Homebrew git strategy" >&2
	echo "       current: $SOURCE_REPO" >&2
	exit 1
fi

VERSION="${TAG#v}"

FORMULA_CONTENT="$(cat <<EOF
class Atlaskb < Formula
  desc "${DESC}"
  homepage "${HOMEPAGE}"
  url "${SOURCE_REPO}",
      tag: "${TAG}",
      revision: "${REVISION}"
  version "${VERSION}"

  depends_on "go" => :build

  def install
    ldflags = "-s -w -X github.com/tgeorge06/atlaskb/internal/version.Version=#{version}"
    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/atlaskb"
  end

  test do
    assert_equal version.to_s, shell_output("#{bin}/atlaskb version").strip
  end
end
EOF
)"

if [[ -n "$OUTPUT" ]]; then
	mkdir -p "$(dirname "$OUTPUT")"
	printf '%s\n' "$FORMULA_CONTENT" > "$OUTPUT"
	echo "wrote formula: $OUTPUT"
else
	printf '%s\n' "$FORMULA_CONTENT"
fi
