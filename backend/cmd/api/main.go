// Command api runs the Хомыч backend. Configuration comes from environment variables
// (see backend/.env.example).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/access"
	"github.com/ilhenyco/ai-buddy/backend/internal/app"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
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

	a := app.New(d, clock.System{}, app.Config{
		RAGServiceToken: os.Getenv("RAG_SERVICE_TOKEN"),
		DemoLogin:       demo,
		SessionSecret:   []byte(secret),
		AllowedOrigins:  strings.Split(env("ALLOWED_ORIGINS", "http://127.0.0.1:4173,http://localhost:4173"), ","),
		SecureCookie:    env("SECURE_COOKIE", "false") == "true",
		Location:        loc,
		AccessService:   accessService,
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
