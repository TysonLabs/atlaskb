package models

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RepoStore struct {
	Pool *pgxpool.Pool
}

func (s *RepoStore) Create(ctx context.Context, r *Repo) error {
	r.ID = uuid.New()
	r.CreatedAt = time.Now()
	r.UpdatedAt = r.CreatedAt
	if r.ExcludeDirs == nil {
		r.ExcludeDirs = []string{}
	}

	_, err := s.Pool.Exec(ctx,
		`INSERT INTO repos (id, name, remote_url, local_path, default_branch, exclude_dirs, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		r.ID, r.Name, r.RemoteURL, r.LocalPath, r.DefaultBranch, r.ExcludeDirs, r.CreatedAt, r.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting repo: %w", err)
	}
	return nil
}

const repoColumns = `id, name, remote_url, local_path, default_branch, exclude_dirs, last_commit_sha, last_indexed_at, created_at, updated_at`

func scanRepo(row pgx.Row) (*Repo, error) {
	r := &Repo{}
	err := row.Scan(&r.ID, &r.Name, &r.RemoteURL, &r.LocalPath, &r.DefaultBranch, &r.ExcludeDirs, &r.LastCommitSHA, &r.LastIndexedAt, &r.CreatedAt, &r.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if r.ExcludeDirs == nil {
		r.ExcludeDirs = []string{}
	}
	return r, nil
}

func (s *RepoStore) GetByID(ctx context.Context, id uuid.UUID) (*Repo, error) {
	r, err := scanRepo(s.Pool.QueryRow(ctx,
		`SELECT `+repoColumns+` FROM repos WHERE id = $1`, id))
	if err != nil {
		return nil, fmt.Errorf("querying repo: %w", err)
	}
	return r, nil
}

func (s *RepoStore) GetByName(ctx context.Context, name string) (*Repo, error) {
	r, err := scanRepo(s.Pool.QueryRow(ctx,
		`SELECT `+repoColumns+` FROM repos WHERE name = $1`, name))
	if err != nil {
		return nil, fmt.Errorf("querying repo by name: %w", err)
	}
	return r, nil
}

func (s *RepoStore) GetByPath(ctx context.Context, path string) (*Repo, error) {
	r, err := scanRepo(s.Pool.QueryRow(ctx,
		`SELECT `+repoColumns+` FROM repos WHERE local_path = $1`, path))
	if err != nil {
		return nil, fmt.Errorf("querying repo by path: %w", err)
	}
	return r, nil
}

func (s *RepoStore) List(ctx context.Context) ([]Repo, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT `+repoColumns+` FROM repos ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listing repos: %w", err)
	}
	defer rows.Close()

	var repos []Repo
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.ID, &r.Name, &r.RemoteURL, &r.LocalPath, &r.DefaultBranch, &r.ExcludeDirs, &r.LastCommitSHA, &r.LastIndexedAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning repo: %w", err)
		}
		if r.ExcludeDirs == nil {
			r.ExcludeDirs = []string{}
		}
		repos = append(repos, r)
	}
	return repos, nil
}

func (s *RepoStore) Update(ctx context.Context, r *Repo) error {
	r.UpdatedAt = time.Now()
	if r.ExcludeDirs == nil {
		r.ExcludeDirs = []string{}
	}
	_, err := s.Pool.Exec(ctx,
		`UPDATE repos SET name = $2, exclude_dirs = $3, updated_at = $4 WHERE id = $1`,
		r.ID, r.Name, r.ExcludeDirs, r.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("updating repo: %w", err)
	}
	return nil
}

func (s *RepoStore) UpdateLastIndexed(ctx context.Context, id uuid.UUID, commitSHA string) error {
	now := time.Now()
	_, err := s.Pool.Exec(ctx,
		`UPDATE repos SET last_commit_sha = $2, last_indexed_at = $3, updated_at = $3 WHERE id = $1`,
		id, commitSHA, now,
	)
	if err != nil {
		return fmt.Errorf("updating repo last indexed: %w", err)
	}
	return nil
}

func (s *RepoStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM repos WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting repo: %w", err)
	}
	return nil
}
