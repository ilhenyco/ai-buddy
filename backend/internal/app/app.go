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

	"github.com/ilhenyco/ai-buddy/backend/internal/access"
	"github.com/ilhenyco/ai-buddy/backend/internal/answers"
	"github.com/ilhenyco/ai-buddy/backend/internal/demo"
	"github.com/ilhenyco/ai-buddy/backend/internal/identity"
	"github.com/ilhenyco/ai-buddy/backend/internal/messages"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/metrics"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/outbox"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
	"github.com/ilhenyco/ai-buddy/backend/internal/sources"
	"github.com/ilhenyco/ai-buddy/backend/internal/suggestions"
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
	// AccessService is the external access request service (the mock in the demo).
	// nil switches the access module off: its routes answer 404.
	AccessService access.External
	// Knowledge is the Python RAG service (knowledge/). nil: questions get an honest
	// "search is not connected" reply instead of an answer.
	Knowledge answers.Knowledge
	// SourcesIndex is the ingestion queue of the same service (a documents:write client key).
	// nil: materials are stored, their index jobs fail with "search is not connected".
	SourcesIndex sources.Index
	// SourcesURLBase + source id is the link stored with an indexed material.
	SourcesURLBase string
	// Events publishes business events from the outbox (EVENTS_PUBLISHER: log, http).
	// nil switches events off: nothing is recorded and no relay runs.
	Events outbox.Publisher
}

type App struct {
	Handler   http.Handler
	Identity  *identity.Service
	Scenarios *scenarios.Service
	Messages  *messages.Service
	Access    *access.Service // nil when Config.AccessService is nil
	Demo      *demo.Service   // nil outside DEMO_MODE
	Answers   *answers.Service
	Sources   *sources.Service
	Metrics   *metrics.Metrics // Prometheus metrics, served at GET /metrics
	Outbox    *outbox.Relay    // nil when Config.Events is nil
	// Clock is the business calendar: scenarios and the scheduler. In demo mode it can be moved
	// (POST /demo/clock); sessions keep the real clock.
	Clock *clock.Offset
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
	calendar := clock.NewOffset(clk)
	// business events are written in the same transaction as the change (platform/outbox)
	var ev *outbox.Writer
	if cfg.Events != nil {
		ev = outbox.NewWriter()
	}
	sc := &scenarios.Service{DB: d, Clock: calendar, Loc: cfg.Location, Events: ev}
	ms := &messages.Service{DB: d, Clock: calendar, Loc: cfg.Location, Plans: sc, Log: log, Events: ev}
	sc.AfterPublish = ms.Trigger

	m := metrics.New()
	mux := http.NewServeMux()
	// outside /api/v1: scraped by Prometheus, not a part of the product API (nginx proxies only /api/)
	mux.Handle("GET /metrics", m.Handler())
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
	var demoClock *clock.Offset
	if cfg.DemoLogin {
		demoClock = calendar
	}
	ms.Routes(mux, APIPrefix, id.Authenticate, demoClock)
	var dm *demo.Service
	if cfg.DemoLogin {
		dm = &demo.Service{DB: d, Calendar: calendar, Loc: cfg.Location,
			Seed:    func(ctx context.Context) error { return scenarios.SeedDemo(ctx, d, calendar, cfg.Location) },
			Deliver: ms.RunOnce}
		ms.SaveDemoDate = dm.SaveDate
		dm.Routes(mux, APIPrefix, id.Authenticate)
	}
	var acc *access.Service
	if cfg.AccessService != nil {
		// real time, not the demo calendar: drafts expire and requests are decided in real time
		acc = &access.Service{DB: d, Clock: clk, External: cfg.AccessService, DraftTTL: 24 * time.Hour,
			InFlight: 5 * time.Second, Metrics: m, Events: ev}
		acc.Routes(mux, APIPrefix, id.Authenticate, cfg.DemoLogin)
		if dm != nil {
			dm.AccessMode = cfg.AccessService.SetMode
		}
	}
	// personal questions: jobs are stored first, the worker calls the knowledge service
	ans := &answers.Service{DB: d, Clock: clk, Calendar: calendar, Knowledge: cfg.Knowledge, Log: log, Metrics: m, Events: ev}
	ans.Routes(mux, APIPrefix, id.Authenticate)
	sg := &suggestions.Service{DB: d, Calendar: calendar, Clock: clk, Loc: cfg.Location}
	sg.Routes(mux, APIPrefix, id.Authenticate)
	if cfg.SourcesURLBase == "" {
		cfg.SourcesURLBase = "https://homych.invalid/sources/"
	}
	src := &sources.Service{DB: d, Clock: clk, Index: cfg.SourcesIndex, URLBase: cfg.SourcesURLBase, Log: log, Metrics: m, Events: ev}
	src.Routes(mux, APIPrefix, id.Authenticate)

	mux.Handle(APIPrefix+"/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return httpx.NotFound()
	}))
	// the metrics middleware wraps the mux directly: httpx.Middleware copies the request, and only
	// the request the mux itself received carries the route pattern (r.Pattern)
	var relay *outbox.Relay
	if cfg.Events != nil {
		// real time: retries of a publisher do not follow the demo calendar
		relay = &outbox.Relay{DB: d, Clock: clk, Publisher: cfg.Events, Log: log, Observer: m}
	}
	return &App{Handler: httpx.Middleware(log, m.Middleware(mux)), Identity: id, Scenarios: sc, Messages: ms, Access: acc, Demo: dm,
		Answers: ans, Sources: src, Metrics: m, Outbox: relay, Clock: calendar}
}
