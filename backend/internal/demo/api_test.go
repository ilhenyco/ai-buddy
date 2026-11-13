package demo_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/apptest"
	"github.com/ilhenyco/ai-buddy/backend/internal/messages"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
)

const base = "/api/v1"

func count(t *testing.T, env *apptest.Env, table string) int {
	t.Helper()
	var n int
	if err := env.DB.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDemoDateSurvivesRestartAndResetStartsOver(t *testing.T) {
	env := apptest.New(t)
	ctx := context.Background()
	loc, _ := time.LoadLocation("Europe/Moscow")
	if err := scenarios.SeedDemo(ctx, env.DB, env.Clock, loc); err != nil {
		t.Fatal(err)
	}
	if _, err := env.App.Messages.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	sasha, alex := env.Login(t, "u-sasha"), env.Login(t, "u-alex")
	var page messages.FeedPage
	sasha.Do(t, "GET", base+"/assignments/assignment-sasha/messages", nil, nil).Decode(t, &page)
	initial := len(page.Items)
	if initial == 0 {
		t.Fatal("no demo messages")
	}

	// people use the stand: a check, an access request, a new scenario, the date moved forward
	m := page.Items[0]
	sasha.Do(t, "PUT", base+"/messages/"+m.ID+"/tasks/"+m.Block.Tasks[0].ID+"/check", map[string]any{"checked": true}, nil)
	sasha.Do(t, "POST", base+"/demo/access-mode", map[string]string{"mode": "deny"}, nil)
	d := sasha.Do(t, "POST", base+"/access-drafts", map[string]any{"assignmentId": "assignment-sasha", "resourceId": "repo-learning-read", "reason": "r"},
		map[string]string{"Idempotency-Key": "k-demo-1"})
	sasha.Do(t, "POST", base+"/access-drafts/"+d.JSON["id"].(string)+"/confirm", map[string]any{"draftVersion": 1, "reason": "r", "confirmed": true},
		map[string]string{"Idempotency-Key": "k-demo-2"})
	alex.Do(t, "POST", base+"/scenarios", map[string]any{"assignmentId": nil, "months": 6, "start": "2026-12-01", "name": "Новый"},
		map[string]string{"Idempotency-Key": "k-demo-3"})
	if r := alex.Do(t, "POST", base+"/demo/clock", map[string]any{"date": "2026-11-26"}, nil); r.Status != http.StatusOK {
		t.Fatalf("clock: %d %s", r.Status, r.Raw)
	}

	// restart of the API: the calendar starts from real time and is restored from the database
	env.App.Clock.Reset()
	if err := env.App.Demo.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if got := clock.Today(env.App.Clock, loc); got != "2026-11-26" {
		t.Fatalf("restored date %s", got)
	}
	if r := alex.Do(t, "POST", base+"/demo/clock", map[string]any{"date": "2026-11-27"}, nil); r.Status != http.StatusOK {
		t.Fatal(r.Status)
	}
	if count(t, env, "demo_settings") != 1 {
		t.Fatal("the date is stored once")
	}

	r := alex.Do(t, "POST", base+"/demo/reset", nil, nil)
	if r.Status != http.StatusOK || r.JSON["shifted"] != false || r.JSON["created"].(float64) != float64(count(t, env, "messages")) {
		t.Fatalf("reset: %d %s", r.Status, r.Raw)
	}
	for table, want := range map[string]int{"scenarios": 3, "message_checks": 0, "access_operations": 0, "access_drafts": 0, "demo_settings": 0} {
		if got := count(t, env, table); got != want {
			t.Errorf("%s: %d, want %d", table, got, want)
		}
	}
	if env.App.Clock.Shifted() {
		t.Error("the date stays moved after reset")
	}
	sasha.Do(t, "GET", base+"/assignments/assignment-sasha/messages", nil, nil).Decode(t, &page)
	if len(page.Items) != initial || len(page.Checks) != 0 {
		t.Fatalf("after reset: %d messages, %d checks", len(page.Items), len(page.Checks))
	}
	// the mock answers "approve" again
	d = sasha.Do(t, "POST", base+"/access-drafts", map[string]any{"assignmentId": "assignment-sasha", "resourceId": "repo-learning-read", "reason": "r"},
		map[string]string{"Idempotency-Key": "k-demo-4"})
	op := sasha.Do(t, "POST", base+"/access-drafts/"+d.JSON["id"].(string)+"/confirm", map[string]any{"draftVersion": 1, "reason": "r", "confirmed": true},
		map[string]string{"Idempotency-Key": "k-demo-5"})
	env.Clock.Set(env.Clock.Now().Add(apptest.MockApproveAfter))
	sasha = env.Login(t, "u-sasha")
	if got := sasha.Do(t, "GET", base+"/access-operations/"+op.JSON["id"].(string), nil, nil); got.JSON["status"] != "approved" {
		t.Fatalf("mock mode after reset: %s", got.Raw)
	}
	if r := env.Anonymous().Do(t, "POST", base+"/demo/reset", nil, nil); r.Status != http.StatusUnauthorized && r.Status != http.StatusForbidden {
		t.Fatalf("anonymous reset: %d", r.Status)
	}
}

func TestRestoreWithoutStoredDate(t *testing.T) {
	env := apptest.New(t)
	if err := env.App.Demo.Restore(context.Background()); err != nil || env.App.Clock.Shifted() {
		t.Fatalf("restore: %v shifted=%v", err, env.App.Clock.Shifted())
	}
}
