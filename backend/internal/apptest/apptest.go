// Package apptest starts the whole API on a fresh database for HTTP-level tests.
// Other modules can reuse it: apptest.New(t).Login(t, "u-alex").Do(...).
package apptest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/access"
	"github.com/ilhenyco/ai-buddy/backend/internal/accessmock"
	"github.com/ilhenyco/ai-buddy/backend/internal/app"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/outbox"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/testdb"
)

// Start of the demo: the first template block is 2026-11-11.
var DefaultNow = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

type Env struct {
	DB     *sql.DB
	Clock  *clock.Manual
	App    *app.App
	Server *httptest.Server
	// AccessMock is the access request service behind the API (approves after MockApproveAfter).
	AccessMock *accessmock.Mock
}

const MockApproveAfter = 20 * time.Second

func New(t testing.TB) *Env {
	t.Helper()
	d := testdb.Open(t)
	if err := app.Prepare(context.Background(), d, true); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	clk := clock.NewManual(DefaultNow)
	mock := accessmock.New(clk, MockApproveAfter)
	mock.LostDelay = 2 * time.Second
	mockSrv := httptest.NewServer(mock.Handler())
	t.Cleanup(mockSrv.Close)
	loc, _ := time.LoadLocation("Europe/Moscow")
	a := app.New(d, clk, app.Config{DemoLogin: true, SessionSecret: []byte("test-secret"),
		AllowedOrigins: []string{"http://127.0.0.1:4173"}, Location: loc,
		AccessService: access.NewHTTPClient(mockSrv.URL, 300*time.Millisecond),
		// business events are recorded in every HTTP test; the relay is driven by tests that need it
		Events: outbox.LogPublisher{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	return &Env{DB: d, Clock: clk, App: a, Server: srv, AccessMock: mock}
}

// Client is a logged-in (or anonymous) browser-like client.
type Client struct {
	env  *Env
	http *http.Client
	CSRF string
}

func (e *Env) Anonymous() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{env: e, http: &http.Client{Jar: jar}}
}

// Login uses the demo login and remembers the CSRF token.
func (e *Env) Login(t testing.TB, userID string) *Client {
	t.Helper()
	c := e.Anonymous()
	resp := c.Do(t, "POST", "/api/v1/demo/session", map[string]string{"userId": userID}, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("login %s: %d %s", userID, resp.Status, resp.Raw)
	}
	c.CSRF = resp.JSON["csrfToken"].(string)
	return c
}

type Response struct {
	Status int
	Header http.Header
	Raw    []byte
	JSON   map[string]any
}

// Do sends a JSON request. Mutations get the CSRF token automatically unless headers override it.
func (c *Client) Do(t testing.TB, method, path string, body any, headers map[string]string) Response {
	t.Helper()
	var rd io.Reader
	if body != nil {
		if raw, ok := body.(string); ok {
			rd = bytes.NewBufferString(raw)
		} else {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
	}
	req, err := http.NewRequest(method, c.env.Server.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.CSRF != "" && method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", c.CSRF)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := Response{Status: resp.StatusCode, Header: resp.Header, Raw: raw}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out.JSON)
	}
	return out
}

// Decode re-decodes the raw body into v.
func (r Response) Decode(t testing.TB, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Raw, v); err != nil {
		t.Fatalf("decode %s: %v", r.Raw, err)
	}
}
