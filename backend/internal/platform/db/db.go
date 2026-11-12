// Package db opens PostgreSQL, applies embedded migrations and runs transactions.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// Open connects and waits for the database (useful right after docker compose up).
func Open(ctx context.Context, url string, wait time.Duration) (*sql.DB, error) {
	d, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(20)
	deadline := time.Now().Add(wait)
	for {
		err = d.PingContext(ctx)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("database unavailable: %w", err)
	}
	return d, nil
}

// Migrate applies *.sql files from fsys in name order, each once, under an advisory lock.
func Migrate(ctx context.Context, d *sql.DB, fsys fs.FS) error {
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	return InTx(ctx, d, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(727274)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
			name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
			return err
		}
		for _, name := range names {
			var done bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, name).Scan(&done); err != nil {
				return err
			}
			if done {
				continue
			}
			body, err := fs.ReadFile(fsys, name)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("migration %s: %w", name, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(name) VALUES ($1)`, name); err != nil {
				return err
			}
		}
		return nil
	})
}

// ExecFile runs a SQL script (used for the demo seed).
func ExecFile(ctx context.Context, d *sql.DB, fsys fs.FS, name string) error {
	body, err := fs.ReadFile(fsys, name)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(body)) == "" {
		return nil
	}
	_, err = d.ExecContext(ctx, string(body))
	return err
}

// InTx runs fn in a transaction and commits if fn returns nil.
func InTx(ctx context.Context, d *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
