// Package metrics exposes Prometheus metrics of the API: HTTP requests and background jobs.
// It owns a private registry (not the global one), so every test and every App gets its own.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

// Job kinds (label "kind" of homych_jobs_total).
const (
	KindAnswer = "answer"
	KindIndex  = "index"
	KindAccess = "access"
)

// Job outcomes (label "outcome" of homych_jobs_total).
const (
	OutcomeCompleted = "completed"
	OutcomeFailed    = "failed"
	OutcomeRetry     = "retry"
)

// Unmatched is the route label of requests that no mux pattern matched.
const Unmatched = "unmatched"

// Metrics holds the registry and the collectors. A nil *Metrics is valid: Job and
// AnswerDuration do nothing, so modules can run without metrics (for example in tests).
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	jobs     *prometheus.CounterVec
	answer   prometheus.Histogram
}

// New creates a Metrics with its own registry.
func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "homych_http_requests_total",
			Help: "HTTP requests by method, route pattern and status.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "homych_http_request_duration_seconds",
			Help:    "HTTP request duration by method and route pattern.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		jobs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "homych_jobs_total",
			Help: "Finished background job steps by kind (answer, index, access) and outcome (completed, failed, retry).",
		}, []string{"kind", "outcome"}),
		answer: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "homych_answer_duration_seconds",
			Help:    "How long a question waits for the answer of the knowledge service (RAG).",
			Buckets: []float64{0.25, 0.5, 1, 2, 5, 10, 20, 30, 60, 120},
		}),
	}
	m.registry.MustRegister(m.requests, m.duration, m.jobs, m.answer)
	// all job series exist from the start (0), so dashboards do not wait for the first job
	for _, k := range []string{KindAnswer, KindIndex, KindAccess} {
		for _, o := range []string{OutcomeCompleted, OutcomeFailed, OutcomeRetry} {
			m.jobs.WithLabelValues(k, o)
		}
	}
	return m
}

// Handler serves the metrics in the Prometheus text format (GET /metrics).
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Job counts a finished job step. Safe on a nil Metrics.
func (m *Metrics) Job(kind, outcome string) {
	if m == nil {
		return
	}
	m.jobs.WithLabelValues(kind, outcome).Inc()
}

// JobCount returns the current value of homych_jobs_total for the labels (0 on a nil Metrics).
// It is meant for tests of the modules that count jobs.
func (m *Metrics) JobCount(kind, outcome string) float64 {
	if m == nil {
		return 0
	}
	var d dto.Metric
	if err := m.jobs.WithLabelValues(kind, outcome).Write(&d); err != nil {
		return 0
	}
	return d.GetCounter().GetValue()
}

// AnswerDuration records how long the knowledge service took to answer. Safe on a nil Metrics.
func (m *Metrics) AnswerDuration(d time.Duration) {
	if m == nil {
		return
	}
	m.answer.Observe(d.Seconds())
}

// Middleware counts every request. The route label is the mux pattern ("GET /api/v1/answers/{answerId}"),
// never the real path, so ids do not create a series each. It must wrap the mux directly: the mux
// sets r.Pattern on the request it receives, and a middleware that copies the request in between
// (like httpx.Middleware) would hide it from an outer wrapper.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		defer func() {
			p := recover()
			if p != nil && !rec.wrote {
				rec.status = http.StatusInternalServerError // the outer middleware answers 500
			}
			route := r.Pattern
			if route == "" {
				route = Unmatched
			}
			m.requests.WithLabelValues(r.Method, route, strconv.Itoa(rec.status)).Inc()
			m.duration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
			if p != nil {
				panic(p)
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status, s.wrote = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wrote = true
	return s.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the real writer (Flush and others).
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
