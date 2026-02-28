package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type RepoInfo struct {
	RootPath      string
	RemoteURL     string
	DefaultBranch string
	HeadCommitSHA string
}

type CommitInfo struct {
	SHA       string
	Author    string
	Date      time.Time
	Message   string
	FilesChanged []string
}

func DetectRepo(path string) (*RepoInfo, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	// Check if .git exists
	gitDir := filepath.Join(absPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("%s is not a git repository", absPath)
	}

	info := &RepoInfo{RootPath: absPath}

	// Get remote URL
	out, err := runGit(absPath, "remote", "get-url", "origin")
	if err == nil {
		info.RemoteURL = strings.TrimSpace(out)
	}

	// Get default branch
	out, err = runGit(absPath, "symbolic-ref", "--short", "HEAD")
	if err == nil {
		info.DefaultBranch = strings.TrimSpace(out)
	} else {
		info.DefaultBranch = "main"
	}

	// Get HEAD commit
	out, err = runGit(absPath, "rev-parse", "HEAD")
	if err == nil {
		info.HeadCommitSHA = strings.TrimSpace(out)
	}

	return info, nil
}

func ParseLog(repoPath string, maxCommits int) ([]CommitInfo, error) {
	// Use COMMIT_BOUNDARY delimiter with full body (%B) instead of subject-only (%s)
	args := []string{"log", fmt.Sprintf("--max-count=%d", maxCommits),
		"--format=COMMIT_BOUNDARY%n%H|%an|%aI%n%B", "--name-only"}
	out, err := runGit(repoPath, args...)
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	var commits []CommitInfo
	// Split on COMMIT_BOUNDARY to get individual commits
	blocks := strings.Split(out, "COMMIT_BOUNDARY")

	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		lines := strings.SplitN(block, "\n", 2)
		if len(lines) < 1 {
			continue
		}

		// First line: SHA|Author|Date
		headerLine := strings.TrimSpace(lines[0])
		parts := strings.SplitN(headerLine, "|", 3)
		if len(parts) < 3 {
			continue
		}

		date, _ := time.Parse(time.RFC3339, parts[2])
		commit := CommitInfo{
			SHA:    parts[0],
			Author: parts[1],
			Date:   date,
		}

		// Remaining lines: body + file names
		if len(lines) > 1 {
			rest := lines[1]
			// The body ends with a blank line, then file names follow
			// Split into body and file list
			bodyAndFiles := strings.Split(rest, "\n")
			var bodyLines []string
			inFiles := false

			for _, l := range bodyAndFiles {
				trimmed := strings.TrimSpace(l)
				if trimmed == "" {
					if len(bodyLines) > 0 {
						// Potential transition from body to files
						inFiles = true
					}
					continue
				}
				// File lines don't start with special chars and don't contain spaces (usually)
				// but body text might. The heuristic: after the first blank line separator,
				// if a line looks like a file path, treat it as such.
				if inFiles && !strings.Contains(trimmed, " ") && (strings.Contains(trimmed, "/") || strings.Contains(trimmed, ".")) {
					commit.FilesChanged = append(commit.FilesChanged, trimmed)
				} else if inFiles && !strings.Contains(trimmed, " ") {
					commit.FilesChanged = append(commit.FilesChanged, trimmed)
				} else {
					if inFiles {
						// Turned out to still be body text, revert
						inFiles = false
					}
					bodyLines = append(bodyLines, l)
				}
			}

			commit.Message = strings.TrimSpace(strings.Join(bodyLines, "\n"))
		}

		if commit.SHA != "" {
			commits = append(commits, commit)
		}
	}

	return commits, nil
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
