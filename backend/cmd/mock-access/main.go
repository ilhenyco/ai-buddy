// Command mock-access runs the demo access request service (see internal/accessmock).
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/accessmock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
)

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8091"
	}
	after, err := time.ParseDuration(os.Getenv("MOCK_APPROVE_AFTER"))
	if err != nil {
		after = 20 * time.Second
	}
	m := accessmock.New(clock.System{}, after)
	if mode := os.Getenv("MOCK_MODE"); mode != "" {
		if err := m.SetMode(mode); err != nil {
			slog.Error("mock-access", "err", err.Error())
			os.Exit(1)
		}
	}
	slog.Info("mock-access listening", "addr", addr, "approveAfter", after.String())
	srv := &http.Server{Addr: addr, Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("mock-access", "err", err.Error())
		os.Exit(1)
	}
}
