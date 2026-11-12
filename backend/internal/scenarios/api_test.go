package scenarios_test

// HTTP tests on a real PostgreSQL. They repeat the rules from
// frontend/tests/scenarios.test.ts and server-handoff.md on the server side.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/apptest"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
)

const base = "/api/v1"

var keyN atomic.Int64 // used from several goroutines in the concurrency test

func key() map[string]string {
	return map[string]string{"Idempotency-Key": fmt.Sprintf("k-%d-%d", time.Now().UnixNano(), keyN.Add(1))}
}

func with(h map[string]string, k, v string) map[string]string {
	out := map[string]string{k: v}
	for a, b := range h {
		out[a] = b
	}
	return out
}

func create(t *testing.T, c *apptest.Client, assignment any, name string) scenarios.Scenario {
	t.Helper()
	r := c.Do(t, "POST", base+"/scenarios", map[string]any{"assignmentId": assignment, "months": 6, "start": "2026-11-11", "name": name}, key())
	if r.Status != http.StatusCreated {
		t.Fatalf("create: %d %s", r.Status, r.Raw)
	}
	var s scenarios.Scenario
	r.Decode(t, &s)
	if r.Header.Get("ETag") != scenarios.ETag(s.DraftVersion) {
		t.Fatalf("etag %q", r.Header.Get("ETag"))
	}
	return s
}

func save(t *testing.T, c *apptest.Client, id string, version int, plan scenarios.Plan) apptest.Response {
	t.Helper()
	return c.Do(t, "PUT", base+"/scenarios/"+id+"/draft", map[string]any{"plan": plan},
		map[string]string{"If-Match": scenarios.ETag(version)})
}

func publish(t *testing.T, c *apptest.Client, id string, version int, expected *string, confirm bool, headers map[string]string) apptest.Response {
	t.Helper()
	if headers == nil {
		headers = key()
	}
	return c.Do(t, "POST", base+"/scenarios/"+id+"/publications",
		map[string]any{"draftVersion": version, "expectedActiveScenarioId": expected, "confirmReplacement": confirm},
		with(headers, "If-Match", scenarios.ETag(version)))
}

func reason(r apptest.Response) string {
	d, _ := r.JSON["details"].(map[string]any)
	s, _ := d["reason"].(string)
	return s
}

func get(t *testing.T, c *apptest.Client, id string) scenarios.Scenario {
	t.Helper()
	r := c.Do(t, "GET", base+"/scenarios/"+id, nil, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("get: %d %s", r.Status, r.Raw)
	}
	var s scenarios.Scenario
	r.Decode(t, &s)
	return s
}

func strp(s string) *string { return &s }

// clone deep-copies a plan: slices inside Plan are shared otherwise.
func clone(t *testing.T, p scenarios.Plan) scenarios.Plan {
	t.Helper()
	var out scenarios.Plan
	b, _ := json.Marshal(p)
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTemplatesAndAccessRules(t *testing.T) {
	env := apptest.New(t)
	anon := env.Anonymous()
	if r := anon.Do(t, "GET", base+"/scenarios", nil, nil); r.Status != http.StatusUnauthorized || r.JSON["code"] != "UNAUTHENTICATED" || r.JSON["requestId"] == "" {
		t.Fatalf("anonymous: %d %s", r.Status, r.Raw)
	}

	lead := env.Login(t, "u-alex")
	r := lead.Do(t, "GET", base+"/scenario-templates", nil, nil)
	var tp scenarios.TemplatePage
	r.Decode(t, &tp)
	if r.Status != 200 || len(tp.Items) != 2 {
		t.Fatalf("templates: %d %s", r.Status, r.Raw)
	}

	intern := env.Login(t, "u-sasha")
	if r := intern.Do(t, "GET", base+"/scenarios", nil, nil); r.Status != http.StatusForbidden {
		t.Fatalf("intern list: %d", r.Status)
	}
	if r := intern.Do(t, "POST", base+"/scenarios", map[string]any{"months": 6, "start": "2026-11-11", "name": "x"}, key()); r.Status != http.StatusForbidden {
		t.Fatalf("intern create: %d", r.Status)
	}

	// CSRF and Origin are required for mutations
	noCSRF := map[string]string{"X-CSRF-Token": "wrong"}
	if r := lead.Do(t, "POST", base+"/scenarios", map[string]any{"months": 6, "start": "2026-11-11", "name": "x"}, with(noCSRF, "Idempotency-Key", "a")); r.Status != http.StatusForbidden {
		t.Fatalf("csrf: %d", r.Status)
	}
	if r := lead.Do(t, "POST", base+"/scenarios", map[string]any{"months": 6, "start": "2026-11-11", "name": "x"}, with(key(), "Origin", "https://evil.example")); r.Status != http.StatusForbidden {
		t.Fatalf("origin: %d", r.Status)
	}
	if r := lead.Do(t, "POST", base+"/scenarios", map[string]any{"months": 6, "start": "2026-11-11", "name": "x"}, with(key(), "Origin", "http://127.0.0.1:4173")); r.Status != http.StatusCreated {
		t.Fatalf("allowed origin: %d %s", r.Status, r.Raw)
	}
}

func TestCreateValidationAndIdempotency(t *testing.T) {
	env := apptest.New(t)
	lead := env.Login(t, "u-alex")

	for _, body := range []any{
		map[string]any{"months": 7, "start": "2026-11-11", "name": "x"},
		map[string]any{"months": 6, "start": "", "name": "x"},
		map[string]any{"months": 6, "start": "2026-11-11", "name": string(make([]rune, 201))},
		map[string]any{"months": 6, "start": "2026-11-11", "name": "x", "extra": 1},
		`{"months":6`,
	} {
		if r := lead.Do(t, "POST", base+"/scenarios", body, key()); r.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%v: %d %s", body, r.Status, r.Raw)
		}
	}
	if r := lead.Do(t, "POST", base+"/scenarios", map[string]any{"months": 6, "start": "2026-11-11", "name": "x"}, nil); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("missing Idempotency-Key: %d", r.Status)
	}
	// another lead's assignment is invisible
	if r := lead.Do(t, "POST", base+"/scenarios", map[string]any{"assignmentId": "assignment-kirill", "months": 6, "start": "2026-11-11", "name": "x"}, key()); r.Status != http.StatusNotFound {
		t.Fatalf("foreign assignment: %d", r.Status)
	}

	k := key()
	body := map[string]any{"assignmentId": "assignment-sasha", "months": 9, "start": "2026-12-01", "name": "Backend · Платформа"}
	first := lead.Do(t, "POST", base+"/scenarios", body, k)
	again := lead.Do(t, "POST", base+"/scenarios", body, k)
	if first.Status != 201 || again.Status != 201 || first.JSON["id"] != again.JSON["id"] || again.Header.Get("ETag") != `"1"` {
		t.Fatalf("idempotent create: %s / %s", first.Raw, again.Raw)
	}
	body["name"] = "other"
	if r := lead.Do(t, "POST", base+"/scenarios", body, k); r.Status != http.StatusConflict || reason(r) != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("reused key: %d %s", r.Status, r.Raw)
	}

	var s scenarios.Scenario
	first.Decode(t, &s)
	if s.Draft.Months != 9 || s.Draft.Blocks[0].Date != "2026-12-01" || *s.Draft.InternID != "u-sasha" ||
		*s.Draft.BuddyID != "u-dasha" || *s.Draft.LeadID != "u-alex" || s.Draft.TeamID != "platform" {
		t.Fatalf("draft not bound to the assignment: %+v", s.Draft)
	}
	if s.Published != nil || s.IsActive || !s.HasUnpublishedChanges || s.DraftVersion != 1 {
		t.Fatalf("new scenario state: %+v", s)
	}

	// a scenario without an intern gets the lead and the lead's team
	free := create(t, lead, nil, "Без стажёра")
	if free.AssignmentID != nil || free.Draft.InternID != nil || *free.Draft.LeadID != "u-alex" || free.Draft.TeamID != "platform" {
		t.Fatalf("free scenario: %+v", free)
	}

	// another lead does not see it
	other := env.Login(t, "u-irina")
	if r := other.Do(t, "GET", base+"/scenarios/"+s.ID, nil, nil); r.Status != http.StatusNotFound {
		t.Fatalf("other lead: %d", r.Status)
	}
	if r := other.Do(t, "PUT", base+"/scenarios/"+s.ID+"/draft", map[string]any{"plan": s.Draft}, map[string]string{"If-Match": `"1"`}); r.Status != http.StatusNotFound {
		t.Fatalf("other lead save: %d", r.Status)
	}
}

func TestSaveDraftVersionsAndIncompleteDrafts(t *testing.T) {
	env := apptest.New(t)
	lead := env.Login(t, "u-alex")
	s := create(t, lead, "assignment-masha", "Маша · черновик")

	// an incomplete draft is saved (test: separate incomplete drafts survive)
	plan := clone(t, s.Draft)
	plan.Blocks[0].Title = ""
	plan.Blocks[0].Intern = ""
	plan.BuddyID = strp("forged") // never trusted
	r := save(t, lead, s.ID, 1, plan)
	if r.Status != 200 || r.Header.Get("ETag") != `"2"` {
		t.Fatalf("save: %d %s", r.Status, r.Raw)
	}
	var saved scenarios.Scenario
	r.Decode(t, &saved)
	if saved.DraftVersion != 2 || saved.Draft.Blocks[0].Title != "" || *saved.Draft.BuddyID != "u-dasha" {
		t.Fatalf("saved: %+v", saved.Draft)
	}

	// the same content again with an old If-Match: idempotent PUT after a lost response
	if r := save(t, lead, s.ID, 1, saved.Draft); r.Status != 200 || r.Header.Get("ETag") != `"2"` {
		t.Fatalf("repeat: %d %s", r.Status, r.Raw)
	}
	// other content with an old If-Match: 412 with the current version
	plan = clone(t, saved.Draft)
	plan.Name = "Другая вкладка"
	r = save(t, lead, s.ID, 1, plan)
	if r.Status != http.StatusPreconditionFailed || r.JSON["code"] != "VERSION_CONFLICT" {
		t.Fatalf("stale: %d %s", r.Status, r.Raw)
	}
	if d := r.JSON["details"].(map[string]any); d["currentVersion"].(float64) != 2 {
		t.Fatalf("details: %v", d)
	}

	if r := lead.Do(t, "PUT", base+"/scenarios/"+s.ID+"/draft", map[string]any{"plan": plan}, nil); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("missing If-Match: %d", r.Status)
	}
	bad := clone(t, saved.Draft)
	bad.Blocks[1].Tasks[0].URL = "ftp://x"
	if r := save(t, lead, s.ID, 2, bad); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("bad url: %d", r.Status)
	}
	foreign := clone(t, saved.Draft)
	foreign.InternID = strp("u-kirill")
	if r := save(t, lead, s.ID, 2, foreign); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("foreign intern: %d %s", r.Status, r.Raw)
	}
	if r := save(t, lead, "scn-missing", 2, saved.Draft); r.Status != http.StatusNotFound {
		t.Fatalf("missing scenario: %d", r.Status)
	}

	// choosing another own intern before publication is allowed
	moved := clone(t, saved.Draft)
	moved.InternID = strp("u-ilya")
	r = save(t, lead, s.ID, 2, moved)
	var m scenarios.Scenario
	r.Decode(t, &m)
	if r.Status != 200 || *m.AssignmentID != "assignment-ilya" {
		t.Fatalf("move before publish: %d %s", r.Status, r.Raw)
	}
	// and removing the intern too
	m.Draft.InternID = nil
	r = save(t, lead, s.ID, m.DraftVersion, m.Draft)
	r.Decode(t, &m)
	if r.Status != 200 || m.AssignmentID != nil || m.Draft.BuddyID != nil {
		t.Fatalf("unassign: %d %s", r.Status, r.Raw)
	}
}

func TestPublishLifecycle(t *testing.T) {
	env := apptest.New(t)
	lead := env.Login(t, "u-alex")
	a := create(t, lead, "assignment-sasha", "Вариант A")

	// incomplete draft cannot be published
	plan := clone(t, a.Draft)
	plan.Blocks[0].Title = ""
	r := save(t, lead, a.ID, 1, plan)
	if r := publish(t, lead, a.ID, 2, nil, false, nil); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("incomplete: %d %s", r.Status, r.Raw)
	}
	plan.Blocks[0].Title = "Первый день"
	r = save(t, lead, a.ID, 2, plan)
	if r.Status != 200 {
		t.Fatalf("save: %s", r.Raw)
	}
	// stale version and mismatched body/header
	if r := publish(t, lead, a.ID, 2, nil, false, nil); r.Status != http.StatusPreconditionFailed {
		t.Fatalf("stale publish: %d", r.Status)
	}
	if r := lead.Do(t, "POST", base+"/scenarios/"+a.ID+"/publications",
		map[string]any{"draftVersion": 3, "expectedActiveScenarioId": nil, "confirmReplacement": false},
		with(key(), "If-Match", `"2"`)); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("header/body mismatch: %d", r.Status)
	}

	// first publication: active, revision 1, no cutoff
	k := key()
	r = publish(t, lead, a.ID, 3, nil, false, k)
	if r.Status != http.StatusCreated {
		t.Fatalf("publish: %d %s", r.Status, r.Raw)
	}
	var pub scenarios.Publication
	r.Decode(t, &pub)
	if !pub.Scenario.IsActive || pub.Scenario.HasUnpublishedChanges || pub.Scenario.Published.Number != 1 ||
		pub.CutoffDate != nil || pub.ReplacedScenarioID != nil {
		t.Fatalf("first publication: %s", r.Raw)
	}
	// the same request again returns the same revision
	again := publish(t, lead, a.ID, 3, nil, false, k)
	var pub2 scenarios.Publication
	again.Decode(t, &pub2)
	if again.Status != 201 || pub2.ActiveRevisionID != pub.ActiveRevisionID || again.Header.Get("ETag") != `"3"` {
		t.Fatalf("idempotent publish: %s", again.Raw)
	}

	// draft edits do not change the published plan (test 27)
	edited := clone(t, pub.Scenario.Draft)
	edited.Blocks[1].Intern = "Новый текст на будущее"
	r = save(t, lead, a.ID, 3, edited)
	cur := get(t, lead, a.ID)
	if !cur.HasUnpublishedChanges || cur.Published.Plan.Blocks[1].Intern == edited.Blocks[1].Intern || !cur.IsActive {
		t.Fatalf("draft leaked into publication: %+v", cur)
	}
	// published intern cannot change (test 73)
	moved := clone(t, cur.Draft)
	moved.InternID = strp("u-masha")
	if r := save(t, lead, a.ID, cur.DraftVersion, moved); r.Status != http.StatusConflict || reason(r) != "PUBLISHED_INTERN_LOCKED" {
		t.Fatalf("intern lock: %d %s", r.Status, r.Raw)
	}

	// second variant for the same intern (test 35, 44)
	env.Clock.Set(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	lead = env.Login(t, "u-alex") // the 8-hour session expired while the clock moved
	b := create(t, lead, "assignment-sasha", "Вариант B")
	if r := publish(t, lead, b.ID, 1, nil, true, nil); r.Status != http.StatusConflict || reason(r) != "ACTIVE_SCENARIO_CHANGED" {
		t.Fatalf("wrong expected: %d %s", r.Status, r.Raw)
	}
	if r := publish(t, lead, b.ID, 1, &a.ID, false, nil); r.Status != http.StatusConflict || reason(r) != "REPLACEMENT_NOT_CONFIRMED" {
		t.Fatalf("no confirmation: %d %s", r.Status, r.Raw)
	}
	r = publish(t, lead, b.ID, 1, &a.ID, true, nil)
	var rep scenarios.Publication
	r.Decode(t, &rep)
	if r.Status != 201 || rep.ReplacedScenarioID == nil || *rep.ReplacedScenarioID != a.ID || rep.CutoffDate == nil || *rep.CutoffDate != "2026-11-20" {
		t.Fatalf("replacement: %d %s", r.Status, r.Raw)
	}
	old := get(t, lead, a.ID)
	if old.IsActive || old.Published == nil {
		t.Fatalf("old variant must stay available but inactive: %+v", old)
	}

	// republishing the active variant keeps the cutoff (test 57)
	env.Clock.Set(time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC))
	lead = env.Login(t, "u-alex")
	again = publish(t, lead, b.ID, 1, &b.ID, false, nil)
	again.Decode(t, &pub2)
	if again.Status != 201 || pub2.Scenario.Published.Number != 2 || pub2.CutoffDate == nil || *pub2.CutoffDate != "2026-11-20" || pub2.ReplacedScenarioID != nil {
		t.Fatalf("republish: %d %s", again.Status, again.Raw)
	}

	// scheduler side: one active plan per assignment, no backfill before the cutoff
	plans, err := env.App.Scenarios.ActivePlans(context.Background())
	if err != nil || len(plans) != 1 || plans[0].ScenarioID != b.ID || plans[0].RevisionNumber != 2 {
		t.Fatalf("active plans: %+v %v", plans, err)
	}
	due := scenarios.DueBlocks(plans[0], "2026-12-01")
	if len(due) != 1 || due[0].ID != "adaptation" {
		t.Fatalf("due after replacement: %+v", due)
	}

	// a scenario without intern cannot be published
	free := create(t, lead, nil, "Без стажёра")
	if r := publish(t, lead, free.ID, 1, nil, false, nil); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("no intern: %d %s", r.Status, r.Raw)
	}
	if r := publish(t, lead, "scn-missing", 1, nil, false, nil); r.Status != http.StatusNotFound {
		t.Fatalf("missing: %d", r.Status)
	}
	if r := lead.Do(t, "POST", base+"/scenarios/"+b.ID+"/publications", map[string]any{"draftVersion": 1}, with(key(), "If-Match", "*")); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("wildcard: %d", r.Status)
	}
}

func TestConcurrentPublicationsKeepOneActive(t *testing.T) {
	env := apptest.New(t)
	lead := env.Login(t, "u-alex")
	ids := []string{create(t, lead, "assignment-ilya", "1").ID, create(t, lead, "assignment-ilya", "2").ID,
		create(t, lead, "assignment-ilya", "3").ID}
	var wg sync.WaitGroup
	codes := make([]int, len(ids))
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			codes[i] = publish(t, lead, id, 1, nil, true, nil).Status
		}(i, id)
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		switch c {
		case 201:
			ok++
		case 409:
		default:
			t.Fatalf("unexpected status %v", codes)
		}
	}
	if ok != 1 {
		t.Fatalf("exactly one first publication must win: %v", codes)
	}
	var n int
	if err := env.DB.QueryRow(`SELECT count(*) FROM active_scenarios WHERE assignment_id='assignment-ilya'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("active rows: %d %v", n, err)
	}
}

func TestListSearchFiltersAndPages(t *testing.T) {
	env := apptest.New(t)
	lead := env.Login(t, "u-alex")
	a := create(t, lead, "assignment-sasha", "Платформа")
	env.Clock.Set(apptest.DefaultNow.Add(time.Minute))
	create(t, lead, "assignment-masha", "Мобилка")
	env.Clock.Set(apptest.DefaultNow.Add(2 * time.Minute))
	create(t, lead, nil, "Пустой")
	env.Clock.Set(apptest.DefaultNow.Add(3 * time.Minute))
	if r := publish(t, lead, a.ID, 1, nil, false, nil); r.Status != 201 {
		t.Fatalf("publish: %s", r.Raw)
	}
	env.Login(t, "u-irina") // other lead's data must not appear
	create(t, env.Login(t, "u-irina"), "assignment-kirill", "Чужой")

	page := func(q string) scenarios.ScenarioPage {
		r := lead.Do(t, "GET", base+"/scenarios"+q, nil, nil)
		if r.Status != 200 {
			t.Fatalf("list %s: %d %s", q, r.Status, r.Raw)
		}
		var p scenarios.ScenarioPage
		r.Decode(t, &p)
		return p
	}
	all := page("")
	if all.Counts != (scenarios.Counts{All: 3, Drafts: 2, Published: 1}) || len(all.Items) != 3 || all.Items[0].ID != a.ID {
		t.Fatalf("all: %+v", all.Counts)
	}
	if p := page("?filter=published"); len(p.Items) != 1 || p.Items[0].ID != a.ID {
		t.Fatalf("published: %+v", p.Items)
	}
	if p := page("?filter=drafts"); len(p.Items) != 2 {
		t.Fatalf("drafts: %d", len(p.Items))
	}
	if p := page("?q=%D0%BC%D0%B0%D1%88%D0%B0"); len(p.Items) != 1 || p.Counts.All != 1 { // "маша" — by intern name
		t.Fatalf("search by intern: %+v", p.Counts)
	}
	p1 := page("?limit=2")
	if len(p1.Items) != 2 || p1.NextCursor == nil {
		t.Fatalf("page 1: %+v", p1)
	}
	p2 := page("?limit=2&cursor=" + *p1.NextCursor)
	if len(p2.Items) != 1 || p2.NextCursor != nil {
		t.Fatalf("page 2: %+v", p2)
	}
	if p := page("?cursor=10"); len(p.Items) != 0 {
		t.Fatal("cursor past the end")
	}
	for _, q := range []string{"?filter=x", "?limit=0", "?limit=101", "?limit=x", "?cursor=-1"} {
		if r := lead.Do(t, "GET", base+"/scenarios"+q, nil, nil); r.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: %d", q, r.Status)
		}
	}
}
