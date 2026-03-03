#!/usr/bin/env bash
# Fetch repos from vector-ventures GitHub org, clone/update, and index into AtlasKB
# Supports parallel repo indexing with --jobs N (default 5)
set -euo pipefail

DRY_RUN=false
FORCE=false
JOBS=5
for arg in "$@"; do
  case "$arg" in
    --dry-run|-n) DRY_RUN=true ;;
    --force|-f)   FORCE=true ;;
    --jobs=*)     JOBS="${arg#--jobs=}" ;;
    -j*)          JOBS="${arg#-j}" ;;
  esac
done

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
echo "Parallel jobs: $JOBS"
if $DRY_RUN; then
  echo "Mode: DRY RUN (no cloning or indexing)"
else
  echo "Logs: $LOGDIR/"
fi
echo ""

# Get already-indexed repos and their commit SHAs from AtlasKB
INDEXED_JSON=$($ATLASKB repos --json 2>/dev/null || echo "[]")

# Counters (use files for atomic updates from subshells)
COUNTDIR=$(mktemp -d)
echo 0 > "$COUNTDIR/succeeded"
echo 0 > "$COUNTDIR/failed"
echo 0 > "$COUNTDIR/skipped"
echo 0 > "$COUNTDIR/count"
trap "rm -rf $COUNTDIR" EXIT

# Increment a counter file atomically
inc() {
  local f="$COUNTDIR/$1"
  local v
  v=$(cat "$f")
  echo $((v + 1)) > "$f"
}

# Function to process a single repo
index_repo() {
  local NAME="$1" FULL_NAME="$2" DEFAULT_BRANCH="$3" PUSHED_AT="$4"
  local REPO_DIR="$CLONE_DIR/$NAME"
  local LOGFILE="$LOGDIR/$NAME.log"
  local NUM="$5"

  # Check if already indexed
  local INDEXED_SHA
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
    local CLONED="no"
    [ -d "$REPO_DIR/.git" ] && CLONED="yes"
    local LAST_PUSH="${PUSHED_AT:0:10}"
    if [ -n "$INDEXED_SHA" ]; then
      echo "[$(date +%H:%M:%S)] ($NUM/$TOTAL) $NAME — cloned: $CLONED  branch: $DEFAULT_BRANCH  pushed: $LAST_PUSH  indexed: $INDEXED_SHA"
    else
      echo "[$(date +%H:%M:%S)] ($NUM/$TOTAL) $NAME — cloned: $CLONED  branch: $DEFAULT_BRANCH  pushed: $LAST_PUSH  indexed: (none — will index)"
    fi
    return
  fi

  # Clone or update
  if [ -d "$REPO_DIR/.git" ]; then
    cd "$REPO_DIR"
    git fetch --quiet 2>&1 || true
    git checkout "$DEFAULT_BRANCH" --quiet 2>&1 || true
    git pull --quiet 2>&1 || true
  else
    echo "[$(date +%H:%M:%S)] ($NUM/$TOTAL) $NAME — cloning..."
    gh repo clone "$FULL_NAME" "$REPO_DIR" -- --quiet 2>&1 || {
      echo "[$(date +%H:%M:%S)] ($NUM/$TOTAL) $NAME — FAILED: clone failed"
      inc failed
      return
    }
    cd "$REPO_DIR"
    git checkout "$DEFAULT_BRANCH" --quiet 2>&1 || true
  fi

  # Get current HEAD SHA
  local HEAD_SHA
  HEAD_SHA=$(git rev-parse HEAD 2>/dev/null || echo "")
  if [ -z "$HEAD_SHA" ]; then
    echo "[$(date +%H:%M:%S)] ($NUM/$TOTAL) $NAME — SKIP: could not determine HEAD"
    inc skipped
    return
  fi

  if [ "$INDEXED_SHA" = "$HEAD_SHA" ] && ! $FORCE; then
    echo "[$(date +%H:%M:%S)] ($NUM/$TOTAL) $NAME — SKIP: already indexed at ${HEAD_SHA:0:8}"
    inc skipped
    return
  fi

  local STATUS="new"
  if $FORCE && [ "$INDEXED_SHA" = "$HEAD_SHA" ]; then
    STATUS="force"
  elif [ -n "$INDEXED_SHA" ]; then
    STATUS="stale"
  fi
  echo "[$(date +%H:%M:%S)] ($NUM/$TOTAL) $NAME — indexing ($STATUS)..."

  # Index
  local INDEX_FLAGS="--yes"
  if $FORCE; then
    INDEX_FLAGS="--yes --force"
  fi
  if $ATLASKB index $INDEX_FLAGS "$REPO_DIR" > "$LOGFILE" 2>&1; then
    local DURATION
    DURATION=$(grep -o 'Indexing complete in [^ ]*' "$LOGFILE" | tail -1 || echo "")
    echo "[$(date +%H:%M:%S)] ($NUM/$TOTAL) $NAME — DONE ${DURATION}"
    inc succeeded
  else
    echo "[$(date +%H:%M:%S)] ($NUM/$TOTAL) $NAME — FAILED (see $LOGFILE)"
    inc failed
  fi
}

# Process repos in parallel
COUNT=0
while IFS=$'\t' read -r NAME FULL_NAME DEFAULT_BRANCH PUSHED_AT; do
  COUNT=$((COUNT + 1))

  # Run in background, limit to $JOBS concurrent
  index_repo "$NAME" "$FULL_NAME" "$DEFAULT_BRANCH" "$PUSHED_AT" "$COUNT" &

  # Throttle: wait if we have $JOBS background jobs
  if (( $(jobs -r | wc -l) >= JOBS )); then
    wait -n 2>/dev/null || true
  fi
done <<< "$REPO_LIST"

# Wait for all remaining jobs
wait

echo ""
echo "════════════════════════════════════════"
if $DRY_RUN; then
  echo "Dry run complete: $TOTAL repos found in $ORG"
else
  echo "Complete: $(cat "$COUNTDIR/succeeded") indexed, $(cat "$COUNTDIR/skipped") skipped, $(cat "$COUNTDIR/failed") failed out of $TOTAL repos"
fi
