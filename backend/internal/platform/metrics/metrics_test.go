package metrics_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/metrics"
)

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/answers/{answerId}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /api/v1/answers", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok")) // implicit 200
	})
	mux.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) { panic("boom") })
	return mux
}

func scrape(t *testing.T, m *metrics.Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics: %d", rec.Code)
	}
	b, _ := io.ReadAll(rec.Body)
	return string(b)
}

func serve(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestMiddlewareUsesRoutePatternAndStatus(t *testing.T) {
	m := metrics.New()
	h := m.Middleware(newMux())
	serve(h, "GET", "/api/v1/answers/ans-1")
	serve(h, "GET", "/api/v1/answers/ans-2") // another id: the same series
	serve(h, "POST", "/api/v1/answers")
	serve(h, "GET", "/nope/123")

	out := scrape(t, m)
	for _, want := range []string{
		`homych_http_requests_total{method="GET",route="GET /api/v1/answers/{answerId}",status="202"} 2`,
		`homych_http_requests_total{method="POST",route="POST /api/v1/answers",status="200"} 1`,
		`homych_http_requests_total{method="GET",route="unmatched",status="404"} 1`,
		`homych_http_request_duration_seconds_count{method="GET",route="GET /api/v1/answers/{answerId}"} 2`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ans-1") || strings.Contains(out, "/nope/123") {
		t.Errorf("real paths must not become labels:\n%s", out)
	}
}

func TestMiddlewareCountsPanicAsServerError(t *testing.T) {
	m := metrics.New()
	h := m.Middleware(newMux())
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic must reach the outer middleware")
			}
		}()
		serve(h, "GET", "/boom")
	}()
	if out := scrape(t, m); !strings.Contains(out, `route="GET /boom",status="500"} 1`) {
		t.Errorf("panic not counted as 500:\n%s", out)
	}
}

func TestMetricsEndpointExposesAllNames(t *testing.T) {
	m := metrics.New()
	mux := newMux()
	mux.Handle("GET /metrics", m.Handler())
	h := m.Middleware(mux)
	serve(h, "GET", "/api/v1/answers/x")
	m.Job(metrics.KindAnswer, metrics.OutcomeCompleted)
	m.AnswerDuration(1500 * time.Millisecond)

	rec := serve(h, "GET", "/metrics")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	out := rec.Body.String()
	for _, name := range []string{
		"homych_http_requests_total", "homych_http_request_duration_seconds",
		"homych_jobs_total", "homych_answer_duration_seconds",
	} {
		if !strings.Contains(out, name) {
			t.Errorf("/metrics lacks %s", name)
		}
	}
	if !strings.Contains(out, `homych_jobs_total{kind="answer",outcome="completed"} 1`) ||
		!strings.Contains(out, `homych_answer_duration_seconds_count 1`) {
		t.Errorf("job or answer duration not counted:\n%s", out)
	}
}

func TestJobSeriesExistFromTheStart(t *testing.T) {
	out := scrape(t, metrics.New())
	for _, kind := range []string{"answer", "index", "access"} {
		for _, outcome := range []string{"completed", "failed", "retry"} {
			want := `homych_jobs_total{kind="` + kind + `",outcome="` + outcome + `"} 0`
			if !strings.Contains(out, want) {
				t.Errorf("missing %s", want)
			}
		}
	}
}

func TestNilMetricsIsSafe(t *testing.T) {
	var m *metrics.Metrics
	m.Job(metrics.KindIndex, metrics.OutcomeFailed)
	m.AnswerDuration(time.Second)
	if m.JobCount(metrics.KindIndex, metrics.OutcomeFailed) != 0 {
		t.Error("a nil Metrics counts nothing")
	}
}

func TestJobCount(t *testing.T) {
	m := metrics.New()
	m.Job(metrics.KindAnswer, metrics.OutcomeRetry)
	m.Job(metrics.KindAnswer, metrics.OutcomeRetry)
	if got := m.JobCount(metrics.KindAnswer, metrics.OutcomeRetry); got != 2 {
		t.Errorf("JobCount = %v, want 2", got)
	}
	if got := m.JobCount(metrics.KindAnswer, metrics.OutcomeFailed); got != 0 {
		t.Errorf("JobCount = %v, want 0", got)
	}
}

func TestRegistriesAreIndependent(t *testing.T) {
	a, b := metrics.New(), metrics.New()
	a.Job(metrics.KindAccess, metrics.OutcomeRetry)
	if strings.Contains(scrape(t, b), `homych_jobs_total{kind="access",outcome="retry"} 1`) {
		t.Error("registries must not share state")
	}
}

func TestResponseControllerReachesWriter(t *testing.T) {
	m := metrics.New()
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush through the recorder: %v", err)
		}
	}))
	serve(h, "GET", "/x")
}
