package app_test

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/lib/pq"

	"github.com/ilhenyco/ai-buddy/backend/internal/app"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
)

// The metrics wiring needs no database: sql.Open is lazy and the requests below never reach a query,
// so this test also runs where PostgreSQL is not available.
func TestMetricsWiringWithoutDatabase(t *testing.T) {
	d, err := sql.Open("postgres", "postgres://nobody:nopass@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	a := app.New(d, clock.System{}, app.Config{SessionSecret: []byte("test-secret")}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(a.Handler)
	defer srv.Close()

	get := func(path string) (int, string) {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	if code, _ := get("/healthz"); code != http.StatusServiceUnavailable { // no database
		t.Fatalf("/healthz without a database: %d", code)
	}
	get("/api/v1/answers/ans-12345/nope") // the API catch-all
	get("/elsewhere/777")                 // matches no pattern

	code, body := get("/metrics")
	if code != http.StatusOK {
		t.Fatalf("/metrics: %d", code)
	}
	for _, want := range []string{
		`homych_http_requests_total{method="GET",route="GET /healthz",status="503"} 1`,
		`homych_http_requests_total{method="GET",route="/api/v1/",status="404"} 1`,
		`homych_http_requests_total{method="GET",route="unmatched",status="404"} 1`,
		"homych_http_request_duration_seconds_bucket",
		`homych_jobs_total{kind="access",outcome="retry"} 0`,
		"homych_answer_duration_seconds_count 0",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics lacks %q", want)
		}
	}
	if strings.Contains(body, "ans-12345") || strings.Contains(body, "777") {
		t.Error("real paths must not become labels")
	}
	if a.Metrics == nil {
		t.Error("App.Metrics is not set")
	}
}
