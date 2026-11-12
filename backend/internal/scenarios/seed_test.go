package scenarios_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/apptest"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
)

func TestDemoSeedScenarios(t *testing.T) {
	env := apptest.New(t)
	ctx := context.Background()
	loc, _ := time.LoadLocation("Europe/Moscow")
	if err := scenarios.SeedDemo(ctx, env.DB, env.Clock, loc); err != nil {
		t.Fatal(err)
	}
	if err := scenarios.SeedDemo(ctx, env.DB, env.Clock, loc); err != nil { // second start: nothing new
		t.Fatal(err)
	}

	lead := env.Login(t, "u-alex")
	var page scenarios.ScenarioPage
	lead.Do(t, "GET", base+"/scenarios", nil, nil).Decode(t, &page)
	if page.Counts != (scenarios.Counts{All: 2, Drafts: 1, Published: 1}) {
		t.Fatalf("Алексей sees his two scenarios only: %+v", page.Counts)
	}
	sasha := get(t, lead, "scn-demo-sasha")
	if !sasha.IsActive || sasha.Published == nil || *sasha.Draft.BuddyID != "u-dasha" || sasha.Draft.Start != "2026-11-05" {
		t.Fatalf("Саша: %+v", sasha)
	}
	// the published plan already has due blocks for the feed
	plans, err := env.App.Scenarios.ActivePlans(ctx)
	if err != nil || len(plans) != 2 {
		t.Fatalf("active plans: %d %v", len(plans), err)
	}
	if due := scenarios.DueBlocks(plans[1], "2026-11-12"); len(due) != 2 || plans[1].AssignmentID != "assignment-sasha" {
		t.Fatalf("due for Саша: %+v", due)
	}

	// Маша's draft is saved but cannot be published until the block title is filled in
	masha := get(t, lead, "scn-demo-masha")
	if masha.Published != nil || masha.Draft.Months != 9 {
		t.Fatalf("Маша: %+v", masha)
	}
	if r := publish(t, lead, masha.ID, masha.DraftVersion, nil, false, nil); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("incomplete demo draft: %d %s", r.Status, r.Raw)
	}

	// Кирилл belongs to Ирина: invisible to Алексей, visible to her
	if r := lead.Do(t, "GET", base+"/scenarios/scn-demo-kirill", nil, nil); r.Status != http.StatusNotFound {
		t.Fatalf("isolation: %d", r.Status)
	}
	if k := get(t, env.Login(t, "u-irina"), "scn-demo-kirill"); !k.IsActive {
		t.Fatalf("Кирилл: %+v", k)
	}
}
