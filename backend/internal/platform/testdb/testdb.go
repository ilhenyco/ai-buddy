// Package testdb gives each test package its own throw-away PostgreSQL database.
// Set TEST_DATABASE_URL to an admin connection, e.g.
// postgres://homych:homych@localhost:5432/postgres?sslmode=disable
package testdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
)

// Open creates a fresh database and drops it when the test ends.
// Without TEST_DATABASE_URL the test is skipped (CI always sets it).
func Open(t testing.TB) *sql.DB {
	t.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	root, err := db.Open(ctx, admin, 10*time.Second)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "homych_t_" + hex.EncodeToString(b)
	if _, err := root.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create db: %v", err)
	}
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	u.Path = "/" + name
	d, err := db.Open(ctx, u.String(), 10*time.Second)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(func() {
		_ = d.Close()
		_, _ = root.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = root.Close()
	})
	return d
}
