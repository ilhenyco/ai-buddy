package suggestions_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/apptest"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
	"github.com/ilhenyco/ai-buddy/backend/internal/suggestions"
)

const path = "/api/v1/assignments/assignment-sasha/suggestions"

func TestSuggestionsFollowTheStage(t *testing.T) {
	env := apptest.New(t)
	sasha := env.Login(t, "u-sasha")
	if r := sasha.Do(t, "GET", path, nil, nil); r.Status != http.StatusConflict || r.JSON["details"].(map[string]any)["reason"] != "NO_ACTIVE_SCENARIO" {
		t.Fatalf("no plan: %d %s", r.Status, r.Raw)
	}
	if err := scenarios.SeedDemo(context.Background(), env.DB, env.Clock, time.UTC); err != nil {
		t.Fatal(err)
	}
	var first suggestions.Selection
	sasha.Do(t, "GET", path, nil, nil).Decode(t, &first)
	if first.StageKey != "block:week" || first.StageKind != "week" || len(first.Questions) != 3 || first.PoolVersion != suggestions.PoolVersion {
		t.Fatalf("first: %+v", first)
	}
	for _, q := range first.Questions {
		if !contains(suggestions.Pools["week"], q) {
			t.Fatalf("not from the pool: %q", q)
		}
	}
	// stable within the stage
	for i := 0; i < 3; i++ {
		var again suggestions.Selection
		sasha.Do(t, "GET", path, nil, nil).Decode(t, &again)
		if !reflect.DeepEqual(again, first) {
			t.Fatalf("changed within the stage: %+v vs %+v", again, first)
		}
	}
	// the next stage comes by the calendar: a new triple of its pool
	env.Clock.Set(env.Clock.Now().Add(8 * 24 * time.Hour))
	sasha = env.Login(t, "u-sasha")
	var next suggestions.Selection
	sasha.Do(t, "GET", path, nil, nil).Decode(t, &next)
	if next.StageKey == first.StageKey || next.StageKind != "adaptation" {
		t.Fatalf("next stage: %+v", next)
	}
	for who, want := range map[string]int{"u-dasha": 403, "u-alex": 403, "u-oleg": 404} {
		if r := env.Login(t, who).Do(t, "GET", path, nil, nil); r.Status != want {
			t.Errorf("%s: %d", who, r.Status)
		}
	}
	if r := sasha.Do(t, "GET", "/api/v1/assignments/assignment-none/suggestions", nil, nil); r.Status != 404 {
		t.Errorf("unknown assignment: %d", r.Status)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestStageKindAndPick(t *testing.T) {
	p := scenarios.Plan{Blocks: []scenarios.Block{
		{ID: "final-prep-1", Date: "2026-12-10"}, {ID: "start", Date: "2026-11-01", StageKind: "start"}, {ID: "custom", Date: "2026-11-20"},
	}}
	cases := map[string][2]string{
		"2026-10-01": {"prestart", "start"}, "2026-11-05": {"block:start", "start"},
		"2026-11-25": {"block:custom", "general"}, "2026-12-11": {"block:final-prep-1", "final-prep"},
	}
	for today, want := range cases {
		if k, kind := suggestions.Stage(p, today); k != want[0] || kind != want[1] {
			t.Errorf("%s: %s %s", today, k, kind)
		}
	}
	if suggestions.Kind(scenarios.Block{ID: "x", StageKind: "midpoint"}) != "midpoint" || suggestions.Kind(scenarios.Block{ID: "final"}) != "final" {
		t.Error("kind")
	}
	identity := func(int) int { return 0 }
	first := suggestions.Pick("week", nil, func(n int) int { return n - 1 }) // no shuffle
	again := suggestions.Pick("week", first, func(n int) int { return n - 1 })
	if reflect.DeepEqual(first, again) || len(again) != 3 {
		t.Fatalf("the same set twice: %v %v", first, again)
	}
	if got := suggestions.Pick("general", nil, identity); len(got) != 3 {
		t.Fatal(got)
	}
}
