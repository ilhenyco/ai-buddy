// Command api runs the Хомыч backend. Configuration comes from environment variables
// (see backend/.env.example).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/access"
	"github.com/ilhenyco/ai-buddy/backend/internal/answers"
	"github.com/ilhenyco/ai-buddy/backend/internal/app"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/outbox"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
	"github.com/ilhenyco/ai-buddy/backend/internal/sources"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("stopped", "err", err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	demo := env("DEMO_MODE", "true") == "true"
	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		if !demo {
			return errors.New("SESSION_SECRET is required outside demo mode")
		}
		secret = "demo-only-secret-change-me"
		log.Warn("SESSION_SECRET not set, using the demo secret")
	}
	loc, err := time.LoadLocation(env("APP_TZ", "Europe/Moscow"))
	if err != nil {
		return err
	}

	d, err := db.Open(ctx, env("DATABASE_URL", "postgres://homych:homych@localhost:5432/homych?sslmode=disable"), 30*time.Second)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := app.Prepare(ctx, d, demo); err != nil {
		return err
	}
	if demo {
		// demo scenarios for Саша, Маша and Кирилл, only into an empty database
		if err := scenarios.SeedDemo(ctx, d, clock.System{}, loc); err != nil {
			return err
		}
	}

	var accessService access.External
	if u := os.Getenv("ACCESS_SERVICE_URL"); u != "" {
		accessService = access.NewHTTPClient(u, 3*time.Second)
	} else {
		log.Warn("ACCESS_SERVICE_URL not set: access requests are switched off")
	}

	// the knowledge service (Python RAG): the same internal token as for /internal/v1/ai-context
	var knowledge answers.Knowledge
	if u, token := os.Getenv("RAG_URL"), os.Getenv("RAG_SERVICE_TOKEN"); u != "" && len(token) >= 24 {
		timeout, err := time.ParseDuration(env("RAG_CLIENT_TIMEOUT", "270s")) // > RAG_REQUEST_TIMEOUT of the service
		if err != nil {
			return err
		}
		knowledge = answers.NewHTTPKnowledge(u, token, timeout)
	} else {
		log.Warn("RAG_URL or RAG_SERVICE_TOKEN not set: questions get a 'search is not connected' reply")
	}

	// materials of the lead go to the same service's ingestion queue with a documents:write key
	var index sources.Index
	if u, key := os.Getenv("RAG_URL"), os.Getenv("RAG_SOURCES_TOKEN"); u != "" && len(key) >= 24 {
		index = sources.NewHTTPIndex(u, key, 15*time.Second)
	}

	// business events (outbox): log by default, http to a collector, off to switch them off
	var events outbox.Publisher
	switch mode := env("EVENTS_PUBLISHER", "log"); mode {
	case "off":
	case "log":
		events = outbox.LogPublisher{Log: log}
	case "http":
		u := os.Getenv("EVENTS_HTTP_URL")
		if u == "" {
			return fmt.Errorf("EVENTS_PUBLISHER=http needs EVENTS_HTTP_URL")
		}
		events = outbox.HTTPPublisher{URL: u, Token: os.Getenv("EVENTS_HTTP_TOKEN")}
	default:
		return fmt.Errorf("EVENTS_PUBLISHER must be off, log or http, got %q", mode)
	}

	a := app.New(d, clock.System{}, app.Config{
		RAGServiceToken: os.Getenv("RAG_SERVICE_TOKEN"),
		DemoLogin:       demo,
		SessionSecret:   []byte(secret),
		AllowedOrigins:  strings.Split(env("ALLOWED_ORIGINS", "http://127.0.0.1:4173,http://localhost:4173"), ","),
		SecureCookie:    env("SECURE_COOKIE", "false") == "true",
		Location:        loc,
		AccessService:   accessService,
		Knowledge:       knowledge,
		SourcesIndex:    index,
		Events:          events,
	}, log)

	if a.Demo != nil {
		// the demo date moved before a restart stays moved
		if err := a.Demo.Restore(ctx); err != nil {
			return err
		}
	}

	// the scheduler: due blocks -> messages, every SCHEDULER_INTERVAL and right after a publication
	interval, err := time.ParseDuration(env("SCHEDULER_INTERVAL", "30s"))
	if err != nil {
		return err
	}
	go a.Messages.Run(ctx, interval)
	go a.Answers.Run(ctx, interval)      // and right after each question
	go a.Sources.Run(ctx, 2*time.Second) // index jobs of materials
	if a.Outbox != nil {
		go a.Outbox.Run(ctx, time.Second) // business events -> publisher
	}

	srv := &http.Server{Addr: env("HTTP_ADDR", ":8080"), Handler: a.Handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("listening", "addr", srv.Addr, "demo", demo)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
