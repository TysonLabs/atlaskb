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

	_, err := s.Pool.Exec(ctx,
		`INSERT INTO repos (id, name, remote_url, local_path, default_branch, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		r.ID, r.Name, r.RemoteURL, r.LocalPath, r.DefaultBranch, r.CreatedAt, r.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting repo: %w", err)
	}
	return nil
}

func (s *RepoStore) GetByID(ctx context.Context, id uuid.UUID) (*Repo, error) {
	r := &Repo{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id, name, remote_url, local_path, default_branch, last_commit_sha, last_indexed_at, created_at, updated_at
		 FROM repos WHERE id = $1`, id,
	).Scan(&r.ID, &r.Name, &r.RemoteURL, &r.LocalPath, &r.DefaultBranch, &r.LastCommitSHA, &r.LastIndexedAt, &r.CreatedAt, &r.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying repo: %w", err)
	}
	return r, nil
}

func (s *RepoStore) GetByPath(ctx context.Context, path string) (*Repo, error) {
	r := &Repo{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id, name, remote_url, local_path, default_branch, last_commit_sha, last_indexed_at, created_at, updated_at
		 FROM repos WHERE local_path = $1`, path,
	).Scan(&r.ID, &r.Name, &r.RemoteURL, &r.LocalPath, &r.DefaultBranch, &r.LastCommitSHA, &r.LastIndexedAt, &r.CreatedAt, &r.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying repo by path: %w", err)
	}
	return r, nil
}

func (s *RepoStore) List(ctx context.Context) ([]Repo, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, name, remote_url, local_path, default_branch, last_commit_sha, last_indexed_at, created_at, updated_at
		 FROM repos ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listing repos: %w", err)
	}
	defer rows.Close()

	var repos []Repo
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.ID, &r.Name, &r.RemoteURL, &r.LocalPath, &r.DefaultBranch, &r.LastCommitSHA, &r.LastIndexedAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning repo: %w", err)
		}
		repos = append(repos, r)
	}
	return repos, nil
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
