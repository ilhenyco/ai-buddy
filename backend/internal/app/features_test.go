package app_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/app"
	"github.com/ilhenyco/ai-buddy/backend/internal/apptest"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/features"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/idempotency"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
)

func strs(v any) []string {
	out := []string{}
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Every flag on (the default): sessionContext lists them and the intern may ask.
func TestFeaturesAllOnByDefault(t *testing.T) {
	env := apptest.New(t)
	r := env.Login(t, "u-sasha").Do(t, "GET", "/api/v1/session/context", nil, nil)
	if r.Status != 200 {
		t.Fatalf("context: %d %s", r.Status, r.Raw)
	}
	if got := strs(r.JSON["features"]); len(got) != 4 || !has(strs(r.JSON["capabilities"]), "questions:ask") {
		t.Fatalf("features %v, capabilities %v", got, r.JSON["capabilities"])
	}
}

// ai_chat, access_integration and events_publisher off: their routes answer 404, the composer
// capability is gone, no business events are recorded; the calendar keeps working.
func TestFeaturesSwitchIntegrationsOff(t *testing.T) {
	set, err := features.Parse("mcp_server")
	if err != nil {
		t.Fatal(err)
	}
	env := apptest.NewWith(t, func(c *app.Config) { c.Features = set })
	ctx := context.Background()
	if err := scenarios.SeedDemo(ctx, env.DB, env.Clock, time.UTC); err != nil {
		t.Fatal(err)
	}
	if n, err := env.App.Messages.RunOnce(ctx); err != nil || n == 0 {
		t.Fatalf("the scheduler works without integrations: %d %v", n, err)
	}
	sasha := env.Login(t, "u-sasha")
	r := sasha.Do(t, "GET", "/api/v1/session/context", nil, nil)
	if got := strs(r.JSON["features"]); len(got) != 1 || got[0] != "mcp_server" {
		t.Fatalf("features: %v", got)
	}
	if has(strs(r.JSON["capabilities"]), "questions:ask") {
		t.Fatal("no composer without ai_chat")
	}
	for _, a := range r.JSON["assignments"].([]any) {
		if has(strs(a.(map[string]any)["capabilities"]), "questions:ask") {
			t.Fatal("no composer in assignments without ai_chat")
		}
	}
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v1/assignments/assignment-sasha/questions"},
		{"GET", "/api/v1/assignments/assignment-sasha/suggestions"},
		{"POST", "/api/v1/access-drafts"},
	} {
		if r := sasha.Do(t, c.method, c.path, map[string]any{"text": "?"}, map[string]string{"Idempotency-Key": "k-" + c.path}); r.Status != http.StatusNotFound {
			t.Fatalf("%s %s: %d", c.method, c.path, r.Status)
		}
	}
	if r := sasha.Do(t, "GET", "/api/v1/assignments/assignment-sasha/messages", nil, nil); r.Status != 200 {
		t.Fatalf("feed: %d", r.Status)
	}
	var events int
	if err := env.DB.QueryRow(`SELECT count(*) FROM outbox_events`).Scan(&events); err != nil || events != 0 {
		t.Fatalf("no events without events_publisher: %d %v", events, err)
	}
	if env.App.Outbox != nil {
		t.Fatal("no relay without events_publisher")
	}
}

func TestIdempotencyRecordsArePurged(t *testing.T) {
	env := apptest.New(t)
	if _, err := env.DB.Exec(`INSERT INTO idempotency_records(subject, operation, key, payload_hash, status, response, created_at) VALUES
		('u-a','op','old','h',200,'{}', now() - interval '8 days'), ('u-a','op','new','h',200,'{}', now())`); err != nil {
		t.Fatal(err)
	}
	n, err := idempotency.Purge(context.Background(), env.DB, 7*24*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("purged %d %v", n, err)
	}
	var left string
	if err := env.DB.QueryRow(`SELECT key FROM idempotency_records`).Scan(&left); err != nil || left != "new" {
		t.Fatalf("left %q %v", left, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	idempotency.PurgeLoop(ctx, env.DB, time.Hour, time.Hour, nil) // returns at once on a done context
}
