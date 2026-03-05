package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectRepo_NonRepo(t *testing.T) {
	_, err := DetectRepo(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "is not a git repository") {
		t.Fatalf("DetectRepo() error = %v, want not-a-git-repo error", err)
	}
}

func TestDetectRepoAndParseLog(t *testing.T) {
	repoDir := t.TempDir()
	runGitCmd(t, repoDir, "init")
	runGitCmd(t, repoDir, "config", "user.email", "test@example.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test User")
	runGitCmd(t, repoDir, "branch", "-m", "main")

	writeFile(t, filepath.Join(repoDir, "a.txt"), "hello\n")
	runGitCmd(t, repoDir, "add", "a.txt")
	runGitCmd(t, repoDir, "commit", "-m", "feat: first\n\nfirst body line")

	writeFile(t, filepath.Join(repoDir, "b.txt"), "world\n")
	runGitCmd(t, repoDir, "add", "b.txt")
	runGitCmd(t, repoDir, "commit", "-m", "feat: second\n\nsecond body line")

	info, err := DetectRepo(repoDir)
	if err != nil {
		t.Fatalf("DetectRepo() error = %v", err)
	}
	if info.RootPath == "" || info.DefaultBranch == "" || info.HeadCommitSHA == "" {
		t.Fatalf("DetectRepo() returned incomplete info: %+v", info)
	}
	if info.DefaultBranch != "main" {
		t.Fatalf("DefaultBranch = %q, want main", info.DefaultBranch)
	}

	commits, err := ParseLog(repoDir, 10)
	if err != nil {
		t.Fatalf("ParseLog() error = %v", err)
	}
	if len(commits) < 2 {
		t.Fatalf("ParseLog() len = %d, want at least 2", len(commits))
	}
	if commits[0].SHA == "" || commits[0].Author == "" {
		t.Fatalf("first commit missing fields: %+v", commits[0])
	}
	if !strings.Contains(commits[0].Message, "second body line") {
		t.Fatalf("first commit message = %q, want second body line", commits[0].Message)
	}
	if !contains(commits[0].FilesChanged, "b.txt") {
		t.Fatalf("first commit files = %+v, want b.txt", commits[0].FilesChanged)
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
