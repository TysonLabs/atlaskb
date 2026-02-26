package git

import (
	"bufio"
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
	args := []string{"log", fmt.Sprintf("--max-count=%d", maxCommits),
		"--format=%H|%an|%aI|%s", "--name-only"}
	out, err := runGit(repoPath, args...)
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	var commits []CommitInfo
	var current *CommitInfo

	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "|", 4)
		if len(parts) == 4 {
			// New commit line
			if current != nil {
				commits = append(commits, *current)
			}
			date, _ := time.Parse(time.RFC3339, parts[2])
			current = &CommitInfo{
				SHA:     parts[0],
				Author:  parts[1],
				Date:    date,
				Message: parts[3],
			}
		} else if current != nil {
			// File path line
			current.FilesChanged = append(current.FilesChanged, line)
		}
	}

	if current != nil {
		commits = append(commits, *current)
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
