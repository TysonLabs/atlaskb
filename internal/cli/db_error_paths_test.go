package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/config"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func newUnreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := "postgres://atlaskb:atlaskb@127.0.0.1:1/atlaskb?sslmode=disable"
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func testCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	return cmd
}

func TestRunAskRepoLookupError(t *testing.T) {
	oldPool, oldAskRepo := pool, askRepo
	t.Cleanup(func() {
		pool = oldPool
		askRepo = oldAskRepo
	})

	pool = newUnreachablePool(t)
	askRepo = "repo-a"

	cmd := testCmd()
	err := runAsk(cmd, []string{"what", "is", "this"})
	if err == nil || !strings.Contains(err.Error(), "listing repos") {
		t.Fatalf("runAsk() error = %v, want listing repos error", err)
	}
}

func TestRunLinkCrossRepoGuard(t *testing.T) {
	oldFromRepo, oldToRepo := linkFromRepo, linkToRepo
	oldFromEntity, oldToEntity := linkFromEntity, linkToEntity
	t.Cleanup(func() {
		linkFromRepo, linkToRepo = oldFromRepo, oldToRepo
		linkFromEntity, linkToEntity = oldFromEntity, oldToEntity
	})

	linkFromRepo = "same"
	linkToRepo = "same"
	linkFromEntity = "a"
	linkToEntity = "b"

	err := runLink(testCmd(), nil)
	if err == nil || !strings.Contains(err.Error(), "cross-repo link requires different repos") {
		t.Fatalf("runLink() error = %v, want cross-repo guard", err)
	}
}

func TestRunLinkResolveRepoError(t *testing.T) {
	oldPool := pool
	oldFromRepo, oldToRepo := linkFromRepo, linkToRepo
	oldFromEntity, oldToEntity := linkFromEntity, linkToEntity
	t.Cleanup(func() {
		pool = oldPool
		linkFromRepo, linkToRepo = oldFromRepo, oldToRepo
		linkFromEntity, linkToEntity = oldFromEntity, oldToEntity
	})

	pool = newUnreachablePool(t)
	linkFromRepo = "repo-a"
	linkToRepo = "repo-b"
	linkFromEntity = "src/a.go"
	linkToEntity = "src/b.go"

	err := runLink(testCmd(), nil)
	if err == nil || !strings.Contains(err.Error(), "listing repos") {
		t.Fatalf("runLink() error = %v, want listing repos error", err)
	}
}

func TestResolveRepoByNameError(t *testing.T) {
	store := &models.RepoStore{Pool: newUnreachablePool(t)}
	_, err := resolveRepoByName(context.Background(), store, "repo-a")
	if err == nil || !strings.Contains(err.Error(), "listing repos") {
		t.Fatalf("resolveRepoByName() error = %v, want listing repos error", err)
	}
}

func TestResolveEntityByPathError(t *testing.T) {
	store := &models.EntityStore{Pool: newUnreachablePool(t)}
	_, err := resolveEntityByPath(context.Background(), store, uuid.New(), "internal/foo.go")
	if err == nil {
		t.Fatal("resolveEntityByPath() expected error, got nil")
	}
}

func TestRunReposError(t *testing.T) {
	oldPool := pool
	t.Cleanup(func() { pool = oldPool })
	pool = newUnreachablePool(t)

	err := runRepos(testCmd(), nil)
	if err == nil || !strings.Contains(err.Error(), "listing repos") {
		t.Fatalf("runRepos() error = %v, want listing repos error", err)
	}
}

func TestRunRetryError(t *testing.T) {
	oldPool := pool
	t.Cleanup(func() { pool = oldPool })
	pool = newUnreachablePool(t)

	err := runRetry(testCmd(), []string{"repo-a"})
	if err == nil || !strings.Contains(err.Error(), "listing repos") {
		t.Fatalf("runRetry() error = %v, want listing repos error", err)
	}
}

func TestRunStatusError(t *testing.T) {
	oldPool := pool
	t.Cleanup(func() { pool = oldPool })
	pool = newUnreachablePool(t)

	err := runStatus(testCmd(), nil)
	if err == nil || !strings.Contains(err.Error(), "listing repos") {
		t.Fatalf("runStatus() error = %v, want listing repos error", err)
	}
}

func TestRunIndexOrchestrateErrorDryRun(t *testing.T) {
	oldPool, oldCfg := pool, cfg
	oldDryRun, oldPhases := indexDryRun, indexPhases
	oldForce, oldYes := indexForce, indexYes
	oldConcurrency := indexConcurrency
	oldExcludes := indexExcludes
	t.Cleanup(func() {
		pool, cfg = oldPool, oldCfg
		indexDryRun, indexPhases = oldDryRun, oldPhases
		indexForce, indexYes = oldForce, oldYes
		indexConcurrency = oldConcurrency
		indexExcludes = oldExcludes
	})

	pool = newUnreachablePool(t)
	cfg = config.DefaultConfig()
	indexDryRun = true
	indexPhases = nil
	indexForce = false
	indexYes = true
	indexConcurrency = 0
	indexExcludes = nil

	cmd := testCmd()
	err := runIndex(cmd, []string{t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "indexing failed") {
		t.Fatalf("runIndex() error = %v, want indexing failed", err)
	}
}
