#!/usr/bin/env bash
# Fetch repos from vector-ventures GitHub org, clone/update, and index into AtlasKB
# Skips repos that are already indexed at the current HEAD commit
set -euo pipefail

DRY_RUN=false
if [[ "${1:-}" == "--dry-run" || "${1:-}" == "-n" ]]; then
  DRY_RUN=true
fi

ORG="vector-ventures"
CLONE_DIR="/Users/tgeorge/src/github.com/vector-ventures"
ATLASKB="$(cd "$(dirname "$0")/.." && pwd)/bin/atlaskb"
LOGDIR="/tmp/atlaskb-index-logs"
mkdir -p "$LOGDIR" "$CLONE_DIR"

# Build binary first
echo "=== Building atlaskb ==="
(cd "$(dirname "$0")/.." && go build -o bin/atlaskb ./cmd/atlaskb)

# Fetch repo list from GitHub org
echo "=== Fetching repos from $ORG ==="
CUTOFF=$(date -v-2y +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -d '2 years ago' --iso-8601=seconds 2>/dev/null)
REPO_LIST=$(gh repo list "$ORG" --no-archived --source --limit 500 --json nameWithOwner,name,defaultBranchRef,pushedAt -q ".[] | select(.pushedAt >= \"$CUTOFF\") | \"\(.name)\t\(.nameWithOwner)\t\(.defaultBranchRef.name // \"main\")\t\(.pushedAt)\"")

if [ -z "$REPO_LIST" ]; then
  echo "No repos found in $ORG (check gh auth status)"
  exit 1
fi

TOTAL=$(echo "$REPO_LIST" | wc -l | tr -d ' ')
echo "Found $TOTAL repos in $ORG"
echo "Clone dir: $CLONE_DIR"
if $DRY_RUN; then
  echo "Mode: DRY RUN (no cloning or indexing)"
else
  echo "Logs: $LOGDIR/"
fi
echo ""

# Get already-indexed repos and their commit SHAs from AtlasKB
INDEXED_JSON=$($ATLASKB repos --json 2>/dev/null || echo "[]")

SUCCEEDED=0
FAILED=0
SKIPPED=0
COUNT=0

while IFS=$'\t' read -r NAME FULL_NAME DEFAULT_BRANCH PUSHED_AT; do
  COUNT=$((COUNT + 1))
  REPO_DIR="$CLONE_DIR/$NAME"
  LOGFILE="$LOGDIR/$NAME.log"

  echo "────────────────────────────────────────"
  echo "[$(date +%H:%M:%S)] ($COUNT/$TOTAL) $NAME"

  # Check if already indexed
  INDEXED_SHA=$(echo "$INDEXED_JSON" | python3 -c "
import json, sys
repos = json.load(sys.stdin)
for r in repos:
    if r.get('local_path','').rstrip('/') == '$(echo "$REPO_DIR" | sed "s/'/\\\\'/g")'.rstrip('/'):
        print(r.get('last_commit_sha') or '')
        sys.exit(0)
print('')
" 2>/dev/null || echo "")

  if $DRY_RUN; then
    CLONED="no"
    [ -d "$REPO_DIR/.git" ] && CLONED="yes"
    LAST_PUSH="${PUSHED_AT:0:10}"
    if [ -n "$INDEXED_SHA" ]; then
      echo "  cloned: $CLONED  branch: $DEFAULT_BRANCH  pushed: $LAST_PUSH  indexed: $INDEXED_SHA"
    else
      echo "  cloned: $CLONED  branch: $DEFAULT_BRANCH  pushed: $LAST_PUSH  indexed: (none — will index)"
    fi
    continue
  fi

  # Clone or update
  if [ -d "$REPO_DIR/.git" ]; then
    echo "  Updating existing clone..."
    cd "$REPO_DIR"
    git fetch --quiet 2>&1 || echo "  Warning: git fetch failed"
    git checkout "$DEFAULT_BRANCH" --quiet 2>&1 || true
    git pull --quiet 2>&1 || echo "  Warning: git pull failed, using current state"
  else
    echo "  Cloning $FULL_NAME..."
    gh repo clone "$FULL_NAME" "$REPO_DIR" -- --quiet 2>&1 || {
      echo "  FAILED: clone failed"
      FAILED=$((FAILED + 1))
      continue
    }
    cd "$REPO_DIR"
    git checkout "$DEFAULT_BRANCH" --quiet 2>&1 || true
  fi

  # Get current HEAD SHA
  HEAD_SHA=$(git rev-parse HEAD 2>/dev/null || echo "")
  if [ -z "$HEAD_SHA" ]; then
    echo "  SKIP: could not determine HEAD"
    SKIPPED=$((SKIPPED + 1))
    continue
  fi

  if [ "$INDEXED_SHA" = "$HEAD_SHA" ]; then
    echo "  SKIP: already indexed at $HEAD_SHA"
    SKIPPED=$((SKIPPED + 1))
    continue
  fi

  if [ -n "$INDEXED_SHA" ]; then
    echo "  Stale index ($INDEXED_SHA -> $HEAD_SHA), re-indexing..."
  else
    echo "  New repo, indexing..."
  fi

  # Index
  if $ATLASKB index --yes "$REPO_DIR" > "$LOGFILE" 2>&1; then
    DURATION=$(grep -o 'Indexing complete in [^ ]*' "$LOGFILE" | tail -1 || echo "")
    echo "  DONE ${DURATION}"
    SUCCEEDED=$((SUCCEEDED + 1))
  else
    echo "  FAILED (see $LOGFILE)"
    FAILED=$((FAILED + 1))
  fi
done <<< "$REPO_LIST"

echo ""
echo "════════════════════════════════════════"
if $DRY_RUN; then
  echo "Dry run complete: $TOTAL repos found in $ORG"
else
  echo "Complete: $SUCCEEDED indexed, $SKIPPED skipped, $FAILED failed out of $TOTAL repos"
fi
