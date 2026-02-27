package db

import (
	"context"
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/config"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Connect(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parsing database config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return pool, nil
}

// HasExistingSchema checks whether AtlasKB tables already exist in the database.
func HasExistingSchema(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema()
			  AND table_name IN ('repos', 'entities', 'facts')
		)`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking existing schema: %w", err)
	}
	return exists, nil
}

// ResetSchema drops all tables, types, and extensions by dropping and
// recreating the public schema. This ensures a completely clean slate.
func ResetSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`)
	if err != nil {
		return fmt.Errorf("resetting schema: %w", err)
	}
	return nil
}

func RunMigrations(dsn string) error {
	newMigrator := func() (*migrate.Migrate, error) {
		source, err := iofs.New(migrationsFS, "migrations")
		if err != nil {
			return nil, fmt.Errorf("creating migration source: %w", err)
		}
		m, err := migrate.NewWithSourceInstance("iofs", source, dsn)
		if err != nil {
			return nil, fmt.Errorf("creating migrator: %w", err)
		}
		return m, nil
	}

	m, err := newMigrator()
	if err != nil {
		return err
	}

	// Check for dirty state before running, and fix it.
	version, dirty, _ := m.Version()
	if dirty {
		// Force the dirty version as clean so Down() can run it.
		if err := m.Force(int(version)); err != nil {
			m.Close()
			return fmt.Errorf("fixing dirty database version %d: %w", version, err)
		}
		// Roll back the dirty migration so it can be re-applied cleanly.
		if err := m.Steps(-1); err != nil {
			m.Close()
			return fmt.Errorf("rolling back dirty migration %d: %w", version, err)
		}
		m.Close()
		// Re-create migrator with fresh source after rollback.
		m, err = newMigrator()
		if err != nil {
			return err
		}
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
}
