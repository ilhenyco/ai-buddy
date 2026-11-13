package messages_test

// HTTP tests on a real PostgreSQL for the calendar messages.
// Demo data: Саша's plan started 2026-11-05 (blocks on 11-05, 11-12, 11-19, ...),
// Кирилл's plan started 2026-11-11 (Ирина's team). Test "today" is 2026-11-12.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/apptest"
	"github.com/ilhenyco/ai-buddy/backend/internal/messages"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
)

const base = "/api/v1"

var keyN atomic.Int64

func key() string { return fmt.Sprintf("k-%d-%d", time.Now().UnixNano(), keyN.Add(1)) }

func setup(t *testing.T) *apptest.Env {
	t.Helper()
	env := apptest.New(t)
	loc, _ := time.LoadLocation("Europe/Moscow")
	if err := scenarios.SeedDemo(context.Background(), env.DB, env.Clock, loc); err != nil {
		t.Fatal(err)
	}
	return env
}

func run(t *testing.T, env *apptest.Env) int {
	t.Helper()
	n, err := env.App.Messages.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func feed(t *testing.T, c *apptest.Client, assignment string) messages.FeedPage {
	t.Helper()
	r := c.Do(t, "GET", base+"/assignments/"+assignment+"/messages", nil, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("feed %s: %d %s", assignment, r.Status, r.Raw)
	}
	var p messages.FeedPage
	r.Decode(t, &p)
	return p
}

func blockIDs(p messages.FeedPage) []string {
	var out []string
	for _, m := range p.Items {
		out = append(out, m.Block.ID)
	}
	return out
}

func check(t *testing.T, c *apptest.Client, msg, task string, value any) apptest.Response {
	t.Helper()
	return c.Do(t, "PUT", base+"/messages/"+msg+"/tasks/"+task+"/check", map[string]any{"checked": value}, nil)
}

// publishDraft saves plan as the draft of id and publishes it (optionally replacing the active variant).
func publishDraft(t *testing.T, c *apptest.Client, id string, mutate func(*scenarios.Plan), expected *string, confirm bool) {
	t.Helper()
	r := c.Do(t, "GET", base+"/scenarios/"+id, nil, nil)
	var s scenarios.Scenario
	r.Decode(t, &s)
	if mutate != nil {
		mutate(&s.Draft)
		r = c.Do(t, "PUT", base+"/scenarios/"+id+"/draft", map[string]any{"plan": s.Draft}, map[string]string{"If-Match": scenarios.ETag(s.DraftVersion)})
		if r.Status != 200 {
			t.Fatalf("save: %d %s", r.Status, r.Raw)
		}
		r.Decode(t, &s)
	}
	r = c.Do(t, "POST", base+"/scenarios/"+id+"/publications",
		map[string]any{"draftVersion": s.DraftVersion, "expectedActiveScenarioId": expected, "confirmReplacement": confirm},
		map[string]string{"If-Match": scenarios.ETag(s.DraftVersion), "Idempotency-Key": key()})
	if r.Status != http.StatusCreated {
		t.Fatalf("publish: %d %s", r.Status, r.Raw)
	}
}

func strp(s string) *string { return &s }

func TestSchedulerSendsEachRoleItsMessageOnce(t *testing.T) {
	env := setup(t)
	first := run(t, env)
	if first == 0 {
		t.Fatal("due demo blocks must produce messages")
	}
	if again := run(t, env); again != 0 {
		t.Fatalf("second run must not repeat: %d", again)
	}

	sasha := feed(t, env.Login(t, "u-sasha"), "assignment-sasha")
	if got := blockIDs(sasha); len(got) != 2 || got[0] != "start" || got[1] != "week" {
		t.Fatalf("Саша: %v", got)
	}
	m := sasha.Items[1]
	if m.Kind != "checkpoint" || m.RecipientUserID != "u-sasha" || m.Text != m.Block.Intern || m.ScenarioID != "scn-demo-sasha" ||
		m.Next == nil || m.Next.ID != "adaptation" || sasha.ActivePlan == nil || sasha.ActivePlan.Start != "2026-11-05" {
		t.Fatalf("message: %+v next=%+v plan=%+v", m, m.Next, sasha.ActivePlan)
	}
	// caught-up messages sit at 09:00 Moscow of their block date
	if want := time.Date(2026, 11, 5, 6, 0, 0, 0, time.UTC); !sasha.Items[0].CreatedAt.Equal(want) {
		t.Fatalf("createdAt %v", sasha.Items[0].CreatedAt)
	}

	dasha := feed(t, env.Login(t, "u-dasha"), "assignment-sasha")
	if len(dasha.Items) != 2 || dasha.Items[0].RecipientUserID != "u-dasha" || dasha.Items[0].Text != dasha.Items[0].Block.Buddy {
		t.Fatalf("Даша: %+v", dasha.Items)
	}
	alex := feed(t, env.Login(t, "u-alex"), "assignment-sasha")
	if len(alex.Items) != 2 || alex.Items[0].Text != alex.Items[0].Block.Lead {
		t.Fatalf("Алексей: %+v", alex.Items)
	}
	// Кирилл (Ирина's team) gets his own first day; Илья has no plan
	if k := feed(t, env.Login(t, "u-kirill"), "assignment-kirill"); len(k.Items) != 1 || k.ActivePlan == nil {
		t.Fatalf("Кирилл: %+v", k)
	}
	if i := feed(t, env.Login(t, "u-ilya"), "assignment-ilya"); len(i.Items) != 0 || i.ActivePlan != nil {
		t.Fatalf("Илья: %+v", i)
	}
}

func TestConcurrentSchedulersDoNotDuplicate(t *testing.T) {
	env := setup(t)
	var wg sync.WaitGroup
	var total atomic.Int64
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := env.App.Messages.RunOnce(context.Background())
			if err != nil {
				errs <- err
			}
			total.Add(int64(n))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var rows int
	if err := env.DB.QueryRow(`SELECT count(*) FROM messages`).Scan(&rows); err != nil || int64(rows) != total.Load() {
		t.Fatalf("rows %d, created %d, %v", rows, total.Load(), err)
	}
	var events int
	_ = env.DB.QueryRow(`SELECT count(*) FROM message_events`).Scan(&events)
	if events != 3 { // Саша: start, week; Кирилл: start
		t.Fatalf("events: %d", events)
	}
}

func TestFeedIsVisibleOnlyToParticipants(t *testing.T) {
	env := setup(t)
	run(t, env)
	for _, who := range []string{"u-kirill", "u-irina", "u-oleg", "u-anna", "u-masha"} {
		if r := env.Login(t, who).Do(t, "GET", base+"/assignments/assignment-sasha/messages", nil, nil); r.Status != http.StatusNotFound {
			t.Fatalf("%s sees Саша's feed: %d", who, r.Status)
		}
	}
	sasha := env.Login(t, "u-sasha")
	if r := sasha.Do(t, "GET", base+"/assignments/nope/messages", nil, nil); r.Status != http.StatusNotFound {
		t.Fatalf("unknown assignment: %d", r.Status)
	}
	if r := env.Anonymous().Do(t, "GET", base+"/assignments/assignment-sasha/messages", nil, nil); r.Status != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", r.Status)
	}
	// pagination
	r := sasha.Do(t, "GET", base+"/assignments/assignment-sasha/messages?limit=1", nil, nil)
	var p messages.FeedPage
	r.Decode(t, &p)
	if len(p.Items) != 1 || p.NextCursor == nil {
		t.Fatalf("page 1: %s", r.Raw)
	}
	sasha.Do(t, "GET", base+"/assignments/assignment-sasha/messages?limit=1&cursor="+*p.NextCursor, nil, nil).Decode(t, &p)
	if len(p.Items) != 1 || p.NextCursor != nil || p.Items[0].Block.ID != "week" {
		t.Fatalf("page 2: %+v", p)
	}
	for _, q := range []string{"?limit=0", "?limit=x", "?limit=500", "?cursor=-1"} {
		if r := sasha.Do(t, "GET", base+"/assignments/assignment-sasha/messages"+q, nil, nil); r.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: %d", q, r.Status)
		}
	}
}

func TestChecksAreOwnAndNeverBlockTheCalendar(t *testing.T) {
	env := setup(t)
	run(t, env)
	sasha, dasha := env.Login(t, "u-sasha"), env.Login(t, "u-dasha")
	week := feed(t, sasha, "assignment-sasha").Items[1]

	r := check(t, sasha, week.ID, "access", true)
	var c messages.Check
	r.Decode(t, &c)
	if r.Status != 200 || !c.Checked || c.UserID != "u-sasha" {
		t.Fatalf("check: %d %s", r.Status, r.Raw)
	}
	if p := feed(t, sasha, "assignment-sasha"); len(p.Checks) != 1 || p.Checks[0].TaskID != "access" {
		t.Fatalf("checks in feed: %+v", p.Checks)
	}
	if r := check(t, sasha, week.ID, "access", false); r.Status != 200 {
		t.Fatalf("uncheck: %d", r.Status)
	}
	if p := feed(t, sasha, "assignment-sasha"); len(p.Checks) != 1 || p.Checks[0].Checked {
		t.Fatalf("unchecked: %+v", p.Checks)
	}
	if r := check(t, sasha, week.ID, "no-such-task", true); r.Status != http.StatusNotFound {
		t.Fatalf("unknown task: %d", r.Status)
	}
	if r := check(t, dasha, week.ID, "access", true); r.Status != http.StatusNotFound {
		t.Fatalf("someone else's message: %d", r.Status)
	}
	if r := check(t, sasha, week.ID, "access", "yes"); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("bad body: %d", r.Status)
	}
	if r := sasha.Do(t, "PUT", base+"/messages/"+week.ID+"/tasks/access/check", map[string]any{}, nil); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("missing checked: %d", r.Status)
	}
	// buddy marks only items that have an action for the buddy
	dashaWeek := feed(t, dasha, "assignment-sasha").Items[1]
	for _, task := range dashaWeek.Block.Tasks {
		r := check(t, dasha, dashaWeek.ID, task.ID, true)
		visible := task.Actions != nil && task.Actions.Buddy != ""
		if (r.Status == 200) != visible {
			t.Fatalf("buddy task %s visible=%v: %d", task.ID, visible, r.Status)
		}
	}

	// nothing was done for the first week, the next block still comes by date
	if err := env.App.Clock.SetToday("2026-11-19", time.FixedZone("MSK", 3*3600)); err != nil {
		t.Fatal(err)
	}
	run(t, env)
	if got := blockIDs(feed(t, sasha, "assignment-sasha")); len(got) != 3 || got[2] != "adaptation" {
		t.Fatalf("calendar must not wait for checks: %v", got)
	}
}

func TestDemoClockReplacementAndSnapshots(t *testing.T) {
	env := setup(t)
	run(t, env)
	alex, sasha := env.Login(t, "u-alex"), env.Login(t, "u-sasha")
	sentWeek := feed(t, sasha, "assignment-sasha").Items[1]

	// editing an already sent block and republishing the same variant does not touch history,
	// while a future block arrives in its new wording
	publishDraft(t, alex, "scn-demo-sasha", func(p *scenarios.Plan) {
		p.Blocks[1].Intern = "Новый текст первой недели"
		p.Blocks[2].Intern = "Цели и обучение — новая редакция"
	}, strp("scn-demo-sasha"), false)
	run(t, env)
	after := feed(t, sasha, "assignment-sasha")
	if len(after.Items) != 2 || after.Items[1].Text != sentWeek.Text {
		t.Fatalf("history changed: %+v", after.Items)
	}

	r := alex.Do(t, "POST", base+"/demo/clock", map[string]any{"date": "2026-11-19"}, nil)
	var st messages.ClockState
	r.Decode(t, &st)
	if r.Status != 200 || st.Today != "2026-11-19" || !st.Shifted || st.Created == 0 {
		t.Fatalf("clock: %d %s", r.Status, r.Raw)
	}
	after = feed(t, sasha, "assignment-sasha")
	if len(after.Items) != 3 || after.Items[2].Text != "Цели и обучение — новая редакция" || after.Items[2].RevisionID == sentWeek.RevisionID {
		t.Fatalf("future block: %+v", after.Items)
	}
	if r := sasha.Do(t, "GET", base+"/demo/clock", nil, nil); !strings.Contains(string(r.Raw), `"today":"2026-11-19"`) {
		t.Fatalf("get clock: %s", r.Raw)
	}
	if r := alex.Do(t, "POST", base+"/demo/clock", map[string]any{"date": "19.11.2026"}, nil); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("bad date: %d", r.Status)
	}

	// a replacement variant does not backfill what is already behind the cutoff
	var created scenarios.Scenario
	alex.Do(t, "POST", base+"/scenarios", map[string]any{"assignmentId": "assignment-sasha", "months": 6, "start": "2026-11-10", "name": "Вариант B"},
		map[string]string{"Idempotency-Key": key()}).Decode(t, &created)
	publishDraft(t, alex, created.ID, nil, strp("scn-demo-sasha"), true) // cutoff 2026-11-19
	run(t, env)
	if got := feed(t, sasha, "assignment-sasha"); len(got.Items) != 3 || got.ActivePlan.ScenarioID != created.ID {
		t.Fatalf("no backfill after replacement: %v %+v", blockIDs(got), got.ActivePlan)
	}
	// B's adaptation block is on 11-24: it arrives once the date comes
	alex.Do(t, "POST", base+"/demo/clock", map[string]any{"date": "2026-11-24"}, nil)
	got := feed(t, sasha, "assignment-sasha")
	if len(got.Items) != 4 || got.Items[3].ScenarioID != created.ID || got.Items[3].Block.ID != "adaptation" {
		t.Fatalf("after replacement: %v", blockIDs(got))
	}

	// back to real time
	alex.Do(t, "POST", base+"/demo/clock", map[string]any{"date": nil}, nil).Decode(t, &st)
	if st.Shifted || st.Today != "2026-11-12" {
		t.Fatalf("reset: %+v", st)
	}
}

func TestNotifications(t *testing.T) {
	env := setup(t)
	run(t, env)
	r := env.Login(t, "u-dasha").Do(t, "GET", base+"/notifications", nil, nil)
	var p messages.NotificationPage
	r.Decode(t, &p)
	if r.Status != 200 || len(p.Items) != 2 || p.Items[0].Title != "Хомыч: Первая неделя · Саша Соколов" || p.Items[0].DeliveryStatus != "sent" {
		t.Fatalf("Даша: %s", r.Raw)
	}
	sasha := env.Login(t, "u-sasha")
	sasha.Do(t, "GET", base+"/notifications?limit=1", nil, nil).Decode(t, &p)
	if len(p.Items) != 1 || p.Items[0].Title != "Хомыч: Первая неделя" || p.NextCursor == nil {
		t.Fatalf("Саша: %+v", p)
	}
	sasha.Do(t, "GET", base+"/notifications?limit=1&cursor="+*p.NextCursor, nil, nil).Decode(t, &p)
	if len(p.Items) != 1 || p.NextCursor != nil || p.Items[0].Title != "Хомыч: Первый день" {
		t.Fatalf("page 2: %+v", p)
	}
	if r := sasha.Do(t, "GET", base+"/notifications?limit=0", nil, nil); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("limit: %d", r.Status)
	}
	if r := sasha.Do(t, "GET", base+"/notifications?cursor=x", nil, nil); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("cursor: %d", r.Status)
	}
}

func TestBackgroundSchedulerWakesUpAfterPublication(t *testing.T) {
	env := apptest.New(t) // no demo scenarios: the first publication starts the calendar
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { env.App.Messages.Run(ctx, time.Hour); close(done) }()

	alex := env.Login(t, "u-alex")
	var s scenarios.Scenario
	alex.Do(t, "POST", base+"/scenarios", map[string]any{"assignmentId": "assignment-ilya", "months": 6, "start": "2026-11-12", "name": "Илья"},
		map[string]string{"Idempotency-Key": key()}).Decode(t, &s)
	publishDraft(t, alex, s.ID, nil, nil, false)

	ilya := env.Login(t, "u-ilya")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if p := feed(t, ilya, "assignment-ilya"); len(p.Items) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduler did not run after the publication")
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done
}
