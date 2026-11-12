// Package app wires modules into one HTTP server (modular monolith).
// To add a module: create internal/<module>, give it Routes(mux, prefix, auth)
// and register it below. Modules talk through Go interfaces, not each other's tables.
package app

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/identity"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
	"github.com/ilhenyco/ai-buddy/backend/migrations"
	"github.com/ilhenyco/ai-buddy/backend/seed"
)

const APIPrefix = "/api/v1"

type Config struct {
	RAGServiceToken string
	DemoLogin       bool
	SessionSecret   []byte
	AllowedOrigins  []string
	SecureCookie    bool
	Location        *time.Location
}

type App struct {
	Handler   http.Handler
	Identity  *identity.Service
	Scenarios *scenarios.Service
}

// Prepare applies migrations and, if requested, the demo seed.
func Prepare(ctx context.Context, d *sql.DB, demoSeed bool) error {
	if err := db.Migrate(ctx, d, migrations.FS); err != nil {
		return err
	}
	if !demoSeed {
		return nil
	}
	names, err := fs.Glob(seed.FS, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, n := range names {
		if err := db.ExecFile(ctx, d, seed.FS, n); err != nil {
			return err
		}
	}
	return nil
}

func New(d *sql.DB, clk clock.Clock, cfg Config, log *slog.Logger) *App {
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	id := &identity.Service{DB: d, Secret: cfg.SessionSecret, AllowedOrigins: cfg.AllowedOrigins,
		Now: clk.Now, SecureCookie: cfg.SecureCookie}
	sc := &scenarios.Service{DB: d, Clock: clk, Loc: cfg.Location}

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := d.PingContext(r.Context()); err != nil {
			return &httpx.Error{Status: http.StatusServiceUnavailable, Code: httpx.CodeUnavailable, Message: "База данных недоступна."}
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return nil
	}))
	id.Routes(mux, APIPrefix, cfg.DemoLogin)
	id.AIRoutes(mux, cfg.RAGServiceToken)
	sc.Routes(mux, APIPrefix, id.Authenticate)
	// Next modules: messages (Бэк-1), answers/knowledge (ML), access mock (Бэк-2).

	mux.Handle(APIPrefix+"/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return httpx.NotFound()
	}))
	return &App{Handler: httpx.Middleware(log, mux), Identity: id, Scenarios: sc}
}
