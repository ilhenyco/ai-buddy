package app_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/app"
	"github.com/ilhenyco/ai-buddy/backend/internal/apptest"
	"github.com/ilhenyco/ai-buddy/backend/internal/identity"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/idempotency"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/testdb"
)

func TestHealthAndUnknownRoutes(t *testing.T) {
	env := apptest.New(t)
	c := env.Anonymous()
	if r := c.Do(t, "GET", "/healthz", nil, nil); r.Status != 200 || r.JSON["status"] != "ok" || r.Header.Get("X-Request-ID") == "" {
		t.Fatalf("health: %d %s", r.Status, r.Raw)
	}
	if r := c.Do(t, "GET", "/api/v1/nope", nil, map[string]string{"X-Request-ID": "req-mine"}); r.Status != 404 || r.JSON["requestId"] != "req-mine" {
		t.Fatalf("unknown: %d %s", r.Status, r.Raw)
	}
	// migrations and seed are repeatable
	if err := app.Prepare(context.Background(), env.DB, true); err != nil {
		t.Fatal(err)
	}
	env.DB.Close()
	if r := c.Do(t, "GET", "/healthz", nil, nil); r.Status != http.StatusServiceUnavailable {
		t.Fatalf("health without db: %d", r.Status)
	}
}

func TestDemoLoginAndSessionContext(t *testing.T) {
	env := apptest.New(t)
	anon := env.Anonymous()

	r := anon.Do(t, "GET", "/api/v1/demo/users", nil, nil)
	if items := r.JSON["items"].([]any); r.Status != 200 || len(items) != 9 {
		t.Fatalf("users: %d %s", r.Status, r.Raw)
	}
	if r := anon.Do(t, "POST", "/api/v1/demo/session", map[string]string{"userId": "nobody"}, nil); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown user: %d", r.Status)
	}
	if r := anon.Do(t, "POST", "/api/v1/demo/session", map[string]string{"userId": "u-alex"}, map[string]string{"Origin": "https://evil.example"}); r.Status != http.StatusForbidden {
		t.Fatalf("login origin: %d", r.Status)
	}
	if r := anon.Do(t, "POST", "/api/v1/demo/session", `{"userId":1}`, nil); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("bad body: %d", r.Status)
	}
	if r := anon.Do(t, "POST", "/api/v1/demo/session", "{}", map[string]string{"Content-Type": "text/plain"}); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("content type: %d", r.Status)
	}
	if r := anon.Do(t, "POST", "/api/v1/demo/session", `{"userId":"u-alex"} {}`, nil); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("trailing data: %d", r.Status)
	}

	lead := env.Login(t, "u-alex")
	r = lead.Do(t, "GET", "/api/v1/session/context", nil, nil)
	var ctx identity.Context
	r.Decode(t, &ctx)
	if r.Status != 200 || ctx.Subject.ID != "u-alex" || len(ctx.Assignments) != 3 || ctx.CSRFToken != lead.CSRF ||
		!slices.Contains(ctx.Capabilities, identity.CapScenariosWrite) || ctx.ServerTime.IsZero() {
		t.Fatalf("lead context: %s", r.Raw)
	}

	buddy := env.Login(t, "u-dasha")
	buddy.Do(t, "GET", "/api/v1/session/context", nil, nil).Decode(t, &ctx)
	if len(ctx.Assignments) != 3 || slices.Contains(ctx.Capabilities, identity.CapScenariosWrite) {
		t.Fatalf("buddy context: %+v", ctx)
	}
	intern := env.Login(t, "u-kirill")
	intern.Do(t, "GET", "/api/v1/session/context", nil, nil).Decode(t, &ctx)
	if len(ctx.Assignments) != 1 || ctx.Assignments[0].Lead.ID != "u-irina" || !slices.Contains(ctx.Capabilities, identity.CapQuestionsAsk) {
		t.Fatalf("intern context: %+v", ctx)
	}
	hr := env.Login(t, "u-anna")
	hr.Do(t, "GET", "/api/v1/session/context", nil, nil).Decode(t, &ctx)
	if len(ctx.Assignments) != 0 || len(ctx.Capabilities) != 0 {
		t.Fatalf("no assignments: %+v", ctx)
	}

	// logout clears the cookie
	if r := lead.Do(t, "DELETE", "/api/v1/demo/session", nil, nil); r.Status != http.StatusNoContent {
		t.Fatalf("logout: %d", r.Status)
	}
	if r := lead.Do(t, "GET", "/api/v1/session/context", nil, nil); r.Status != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", r.Status)
	}

	// sessions expire after 8 hours
	env.Clock.Set(apptest.DefaultNow.Add(9 * time.Hour))
	if r := buddy.Do(t, "GET", "/api/v1/session/context", nil, nil); r.Status != http.StatusUnauthorized {
		t.Fatalf("expired: %d", r.Status)
	}
}

func TestSessionCookieCannotBeForged(t *testing.T) {
	env := apptest.New(t)
	s := env.App.Identity
	valid := s.NewSessionValue("u-alex")
	payload, sig, _ := strings.Cut(valid, ".")
	for name, v := range map[string]string{
		"no dot":        "abc",
		"bad base64":    "!!!." + sig,
		"other user":    "dS1zYXNoYXwxOTk5OTk5OTk5." + sig,
		"bad signature": payload + ".xxx",
	} {
		req, _ := http.NewRequest("GET", env.Server.URL+"/api/v1/session/context", nil)
		req.AddCookie(&http.Cookie{Name: identity.CookieName, Value: v})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: %d", name, resp.StatusCode)
		}
	}
	// a correctly signed cookie for a user that no longer exists
	ghost := s.NewSessionValue("u-ghost")
	req, _ := http.NewRequest("GET", env.Server.URL+"/api/v1/session/context", nil)
	req.AddCookie(&http.Cookie{Name: identity.CookieName, Value: ghost})
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ghost: %d", resp.StatusCode)
	}
	if _, err := identity.LeadAssignmentByIntern(context.Background(), env.DB, "u-alex", "u-kirill"); err == nil {
		t.Fatal("foreign intern must be not found")
	}
	if _, err := s.Person(context.Background(), "u-ghost"); err == nil {
		t.Fatal("unknown person")
	}
}

func TestIdempotencyKeyHeader(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", nil)
	if _, err := idempotency.Key(req); err == nil {
		t.Fatal("missing key")
	}
	req.Header.Set("Idempotency-Key", strings.Repeat("k", idempotency.MaxKeyLen+1))
	if _, err := idempotency.Key(req); err == nil {
		t.Fatal("too long key")
	}
	if _, err := idempotency.Hash(func() {}); err == nil {
		t.Fatal("unserialisable payload")
	}
}

func TestMigrationErrorIsReported(t *testing.T) {
	d := testdb.Open(t)
	if _, err := d.Exec(`CREATE TABLE users(id int)`); err != nil {
		t.Fatal(err)
	}
	if err := app.Prepare(context.Background(), d, false); err == nil || !strings.Contains(err.Error(), "0001_identity.sql") {
		t.Fatalf("want migration error, got %v", err)
	}
}
