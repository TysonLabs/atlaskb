package models

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultStaleAfter is the maximum age before indexed data is considered stale.
	DefaultStaleAfter = 7 * 24 * time.Hour
)

type RepoStaleness struct {
	RepoID        uuid.UUID `json:"repo_id"`
	Stale         bool      `json:"stale"`
	Reasons       []string  `json:"reasons,omitempty"`
	CommitsBehind *int      `json:"commits_behind,omitempty"`
}

// ComputeRepoStaleness evaluates basic staleness triggers:
// never indexed, index age, and commit drift from current HEAD.
func ComputeRepoStaleness(ctx context.Context, repo Repo) RepoStaleness {
	out := RepoStaleness{RepoID: repo.ID}

	if repo.LastIndexedAt == nil {
		out.Stale = true
		out.Reasons = append(out.Reasons, "never_indexed")
		return out
	}

	if time.Since(*repo.LastIndexedAt) > DefaultStaleAfter {
		out.Stale = true
		out.Reasons = append(out.Reasons, "index_age")
	}

	if repo.LocalPath == "" || repo.LastCommitSHA == nil || strings.TrimSpace(*repo.LastCommitSHA) == "" {
		return out
	}

	headOut, err := exec.CommandContext(ctx, "git", "-C", repo.LocalPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return out
	}
	head := strings.TrimSpace(string(headOut))
	if head == "" || head == *repo.LastCommitSHA {
		return out
	}

	out.Stale = true
	out.Reasons = append(out.Reasons, "commit_drift")

	countOut, err := exec.CommandContext(ctx, "git", "-C", repo.LocalPath, "rev-list", "--count", *repo.LastCommitSHA+"..HEAD").Output()
	if err == nil {
		if n, convErr := strconv.Atoi(strings.TrimSpace(string(countOut))); convErr == nil {
			out.CommitsBehind = Ptr(n)
		}
	}
	return out
}
