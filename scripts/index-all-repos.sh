#!/usr/bin/env bash
# Discover, update, and re-index all repos in vector-ventures
set -euo pipefail

ATLASKB="$(cd "$(dirname "$0")/.." && pwd)/bin/atlaskb"
VV="/Users/tgeorge/src/github.com/vector-ventures"
LOGDIR="/tmp/atlaskb-index-logs"
mkdir -p "$LOGDIR"

# Build binary first
echo "=== Building atlaskb ==="
(cd "$(dirname "$0")/.." && go build -o bin/atlaskb ./cmd/atlaskb)

# Discover all git repos
REPOS=()
for dir in "$VV"/*/; do
  [ -d "$dir/.git" ] && REPOS+=("$dir")
done

echo "=== AtlasKB Full Re-index ==="
echo "Found ${#REPOS[@]} repos in $VV"
echo "Logs: $LOGDIR/"
echo ""
ls -l "$VV"
echo ""

SUCCEEDED=0
FAILED=0
for i in "${!REPOS[@]}"; do
  DIR="${REPOS[$i]}"
  NAME="$(basename "$DIR")"
  LOGFILE="$LOGDIR/$NAME.log"

  echo "────────────────────────────────────────"
  echo "[$(date +%H:%M:%S)] ($((i+1))/${#REPOS[@]}) $NAME"

  # Checkout main/master and pull latest
  cd "$DIR"
  DEFAULT_BRANCH=$(git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null | sed 's@^refs/remotes/origin/@@' || echo "")
  if [ -z "$DEFAULT_BRANCH" ]; then
    if git show-ref --verify --quiet refs/heads/main; then
      DEFAULT_BRANCH="main"
    elif git show-ref --verify --quiet refs/heads/master; then
      DEFAULT_BRANCH="master"
    else
      echo "  SKIP: no main/master branch found"
      continue
    fi
  fi

  echo "  Checking out $DEFAULT_BRANCH and pulling..."
  git checkout "$DEFAULT_BRANCH" --quiet 2>&1 || true
  git pull --quiet 2>&1 || echo "  Warning: git pull failed, indexing current state"

  # Index
  echo "  Indexing (force)..."
  if $ATLASKB index --force --yes "$DIR" > "$LOGFILE" 2>&1; then
    DURATION=$(grep -o 'Indexing complete in [^ ]*' "$LOGFILE" | tail -1 || echo "")
    echo "  DONE ${DURATION}"
    SUCCEEDED=$((SUCCEEDED + 1))
  else
    echo "  FAILED (see $LOGFILE)"
    FAILED=$((FAILED + 1))
  fi
done

echo ""
echo "════════════════════════════════════════"
echo "Complete: $SUCCEEDED succeeded, $FAILED failed out of ${#REPOS[@]} repos"
