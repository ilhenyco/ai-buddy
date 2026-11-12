package scenarios

import (
	"errors"
	"testing"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
)

func strp(s string) *string { return &s }

func fullPlan(t *testing.T) Plan {
	t.Helper()
	p, err := FromTemplate(6, "2026-11-11")
	if err != nil {
		t.Fatal(err)
	}
	p.Name = "Backend · Платформа"
	p.InternID, p.BuddyID, p.LeadID, p.TeamID = strp("u-sasha"), strp("u-dasha"), strp("u-alex"), "platform"
	return p
}

func fields(t *testing.T, err error) []string {
	t.Helper()
	var e *httpx.Error
	if !errors.As(err, &e) || e.Code != httpx.CodeValidation {
		t.Fatalf("want validation error, got %v", err)
	}
	if e.Details == nil {
		return nil
	}
	return e.Details["fields"].([]string)
}

func TestTemplatesMatchFrontend(t *testing.T) {
	ts := Templates()
	if len(ts) != 2 || ts[0].Months != 6 || ts[1].Months != 9 {
		t.Fatalf("templates: %+v", ts)
	}
	for _, tpl := range ts {
		if len(tpl.Blocks) != 8 || tpl.PoolVersion == "" || tpl.Blocks[0].Date != "2026-11-11" {
			t.Fatalf("template %s is not the frontend makePlan()", tpl.ID)
		}
	}
	// cloned: changing the result does not change the built-in template
	ts[0].Blocks[0].Title = "changed"
	if Templates()[0].Blocks[0].Title == "changed" {
		t.Fatal("templates must be copied")
	}
}

func TestFromTemplateShiftsDates(t *testing.T) {
	p, err := FromTemplate(9, "2026-12-01")
	if err != nil {
		t.Fatal(err)
	}
	if p.Start != "2026-12-01" || p.Blocks[0].Date != "2026-12-01" || p.Blocks[1].Date != "2026-12-08" {
		t.Fatalf("not shifted: %s %s %s", p.Start, p.Blocks[0].Date, p.Blocks[1].Date)
	}
	if _, err := FromTemplate(7, "2026-12-01"); err == nil {
		t.Fatal("7 months must fail")
	}
	if _, err := FromTemplate(6, "bad"); err == nil {
		t.Fatal("bad date must fail")
	}
	if shiftDate("", 3) != "" {
		t.Fatal("empty date stays empty")
	}
}

func TestValidateStructureAllowsIncompleteDraft(t *testing.T) {
	p := fullPlan(t)
	p.Name, p.Blocks[0].Title, p.Blocks[0].Date, p.Blocks[0].Intern, p.Start = "", "", "", "", ""
	p.InternID = nil
	if err := ValidateStructure(p); err != nil {
		t.Fatalf("incomplete draft must be accepted: %v", err)
	}
	if err := ValidateForPublish(p); err == nil {
		t.Fatal("incomplete draft must not be published")
	} else {
		got := fields(t, err)
		want := map[string]bool{"name": true, "internId": true, "start": true, "blocks[0].title": true, "blocks[0].date": true, "blocks[0].intern": true}
		for _, f := range got {
			delete(want, f)
		}
		if len(want) > 0 {
			t.Fatalf("missing fields %v in %v", want, got)
		}
	}
}

func TestValidateStructureRejectsBadData(t *testing.T) {
	cases := map[string]func(p *Plan){
		"months":                   func(p *Plan) { p.Months = 7 },
		"start":                    func(p *Plan) { p.Start = "11.11.2026" },
		"blocks":                   func(p *Plan) { p.Blocks = nil },
		"blocks[1].id":             func(p *Plan) { p.Blocks[1].ID = p.Blocks[0].ID },
		"blocks[0].stageKind":      func(p *Plan) { p.Blocks[0].StageKind = "x" },
		"blocks[0].date":           func(p *Plan) { p.Blocks[0].Date = "2026-13-01" },
		"blocks[0].endDate":        func(p *Plan) { p.Blocks[0].EndDate = "tomorrow" },
		"blocks[0].tasks[0].url":   func(p *Plan) { p.Blocks[0].Tasks[0].URL = "javascript:alert(1)" },
		"blocks[0].tasks[0].kind":  func(p *Plan) { p.Blocks[0].Tasks[0].Kind = "intern" },
		"blocks[0].tasks[1].id":    func(p *Plan) { p.Blocks[0].Tasks[1].ID = p.Blocks[0].Tasks[0].ID },
		"blocks[0].tasks[0].title": func(p *Plan) { p.Blocks[0].Tasks[0].Title = string(make([]rune, 201)) },
		"blocks[0].intern": func(p *Plan) {
			p.Blocks[0].Intern = string(make([]rune, maxText+1))
		},
		"blocks[0].tasks[0].actions": func(p *Plan) {
			p.Blocks[0].Tasks[0].Actions = &RoleActions{Buddy: string(make([]rune, maxText+1))}
		},
		"blocks[0].tasks[0].description": func(p *Plan) {
			p.Blocks[0].Tasks[0].Description = string(make([]rune, maxText+1))
		},
		"blocks[0].title": func(p *Plan) { p.Blocks[0].Title = string(make([]rune, maxTitle+1)) },
		"name":            func(p *Plan) { p.Name = string(make([]rune, maxNameRunes+1)) },
		"blocks[0].tasks": func(p *Plan) {
			for i := 0; i <= maxTasks; i++ {
				p.Blocks[0].Tasks = append(p.Blocks[0].Tasks, Task{ID: "t" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Kind: "team"})
			}
		},
	}
	for want, mutate := range cases {
		t.Run(want, func(t *testing.T) {
			p := fullPlan(t)
			mutate(&p)
			got := fields(t, ValidateStructure(p))
			found := false
			for _, f := range got {
				found = found || f == want
			}
			if !found {
				t.Fatalf("want %s in %v", want, got)
			}
			if ValidateForPublish(p) == nil {
				t.Fatal("publish must fail too")
			}
		})
	}
}

func TestValidateForPublishRequiresTaskTitles(t *testing.T) {
	p := fullPlan(t)
	if err := ValidateForPublish(p); err != nil {
		t.Fatalf("template plan must be publishable: %v", err)
	}
	p.Blocks[2].Tasks[0].Title = "  "
	if got := fields(t, ValidateForPublish(p)); len(got) != 1 || got[0] != "blocks[2].tasks[0].title" {
		t.Fatalf("got %v", got)
	}
}

func TestValidExternalURL(t *testing.T) {
	for url, ok := range map[string]bool{
		"https://avito.ru/x": true, "http://example.org": true,
		"ftp://example.org": false, "https://": false, "not a url": false, "javascript:alert(1)": false,
	} {
		if ValidExternalURL(url) != ok {
			t.Errorf("%q: want %v", url, ok)
		}
	}
}

func TestParseIfMatch(t *testing.T) {
	for in, want := range map[string]int{`"3"`: 3, `W/"4"`: 4, "5": 5} {
		got, err := ParseIfMatch(in)
		if err != nil || got != want {
			t.Errorf("%q: got %d %v", in, got, err)
		}
	}
	for _, in := range []string{"", "*", `"0"`, `"x"`} {
		if _, err := ParseIfMatch(in); err == nil {
			t.Errorf("%q must fail", in)
		}
	}
	if ETag(7) != `"7"` {
		t.Fatal("etag format")
	}
}

func TestDueBlocksRespectsCutoff(t *testing.T) {
	p := ActivePlan{Plan: fullPlan(t)}
	if got := DueBlocks(p, "2026-11-20"); len(got) != 2 || got[0].ID != "start" || got[1].ID != "week" {
		t.Fatalf("due: %+v", got)
	}
	p.CutoffDate = strp("2026-11-18")
	if got := DueBlocks(p, "2026-11-20"); len(got) != 0 {
		t.Fatalf("replacement must not backfill: %+v", got)
	}
	if got := DueBlocks(p, "2026-11-25"); len(got) != 1 || got[0].ID != "adaptation" {
		t.Fatalf("after cutoff: %+v", got)
	}
	p.Plan.Blocks[3].Date = ""
	if got := DueBlocks(p, "2030-01-01"); len(got) != 5 {
		t.Fatalf("undated block must be skipped: %d", len(got))
	}
}
