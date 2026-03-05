package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/config"
)

func unreachableDBConfig() config.DatabaseConfig {
	return config.DatabaseConfig{
		Host:     "127.0.0.1",
		Port:     1,
		User:     "atlaskb",
		Password: "atlaskb",
		DBName:   "atlaskb",
		SSLMode:  "disable",
	}
}

func TestConnectPingFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	pool, err := Connect(ctx, unreachableDBConfig())
	if err == nil {
		if pool != nil {
			pool.Close()
		}
		t.Fatal("Connect() expected error for unreachable DB, got nil")
	}
	if !strings.Contains(err.Error(), "pinging database") {
		t.Fatalf("Connect() error = %v, want pinging database error", err)
	}
}

func TestHasExistingSchemaError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(unreachableDBConfig().DSN())
	if err != nil {
		t.Fatalf("ParseConfig error: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWithConfig error: %v", err)
	}
	defer pool.Close()

	_, err = HasExistingSchema(ctx, pool)
	if err == nil || !strings.Contains(err.Error(), "checking existing schema") {
		t.Fatalf("HasExistingSchema() error = %v, want wrapped schema error", err)
	}
}

func TestResetSchemaError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(unreachableDBConfig().DSN())
	if err != nil {
		t.Fatalf("ParseConfig error: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWithConfig error: %v", err)
	}
	defer pool.Close()

	err = ResetSchema(ctx, pool)
	if err == nil || !strings.Contains(err.Error(), "resetting schema") {
		t.Fatalf("ResetSchema() error = %v, want wrapped reset error", err)
	}
}

func TestRunMigrationsInvalidDSN(t *testing.T) {
	err := RunMigrations("postgres://%")
	if err == nil {
		t.Fatal("RunMigrations() expected error for invalid DSN, got nil")
	}
}
