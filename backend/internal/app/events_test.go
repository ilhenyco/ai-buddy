package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/answers"
	"github.com/ilhenyco/ai-buddy/backend/internal/apptest"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/outbox"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
)

type knowledgeStub struct{}

func (knowledgeStub) Answer(_ context.Context, in answers.RAGInput) (answers.RAGResult, error) {
	return answers.RAGResult{RequestID: in.RequestID, Status: "answered", Reply: "Ответ по материалам.", RetrievalMode: "hybrid",
		Citations: []answers.RAGCitation{{DocumentID: "doc-1", Revision: "r1", Title: "Памятка", Section: "Раздел", URL: "https://example.org/a"}}}, nil
}

type stored struct {
	Type, Aggregate, Assignment, Team, Actor, Role string
	Data                                           map[string]any
}

func events(t *testing.T, env *apptest.Env, typ string) []stored {
	t.Helper()
	rows, err := env.DB.Query(`SELECT type, aggregate_type || '/' || aggregate_id, COALESCE(assignment_id,''), COALESCE(team_id,''),
		COALESCE(actor_id,''), COALESCE(actor_role,''), payload FROM outbox_events WHERE type=$1 ORDER BY created_at, id`, typ)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []stored
	for rows.Next() {
		var e stored
		var raw []byte
		if err := rows.Scan(&e.Type, &e.Aggregate, &e.Assignment, &e.Team, &e.Actor, &e.Role, &raw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &e.Data); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

var n int

func k() map[string]string {
	n++
	return map[string]string{"Idempotency-Key": fmt.Sprintf("ev-%d-%d", time.Now().UnixNano(), n)}
}

// Every business change of the main path records its event in the same transaction, once,
// with the assignment and team, and without the private texts of the intern.
func TestBusinessEventsOfTheMainPath(t *testing.T) {
	env := apptest.New(t)
	ctx := context.Background()
	if err := scenarios.SeedDemo(ctx, env.DB, env.Clock, time.UTC); err != nil {
		t.Fatal(err)
	}
	env.App.Answers.Knowledge = knowledgeStub{}

	// scheduler: one stage.delivered per block, never repeated
	if _, err := env.App.Messages.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	delivered := len(events(t, env, outbox.StageDelivered))
	if delivered == 0 {
		t.Fatal("due blocks must record stage.delivered")
	}
	if _, err := env.App.Messages.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	st := events(t, env, outbox.StageDelivered)
	if len(st) != delivered {
		t.Fatalf("a second run must not repeat events: %d -> %d", delivered, len(st))
	}
	sashaStage := st[0]
	for _, e := range st {
		if e.Assignment == "assignment-sasha" {
			sashaStage = e
		}
	}
	if sashaStage.Assignment != "assignment-sasha" || sashaStage.Team != "platform" || sashaStage.Actor != "" ||
		sashaStage.Data["blockId"] == nil || len(sashaStage.Data["recipients"].([]any)) == 0 {
		t.Fatalf("stage event: %+v", sashaStage)
	}

	// publication by the lead
	alex := env.Login(t, "u-alex")
	r := alex.Do(t, "GET", "/api/v1/scenarios/scn-demo-sasha", nil, nil)
	var sc scenarios.Scenario
	r.Decode(t, &sc)
	h := k()
	h["If-Match"] = scenarios.ETag(sc.DraftVersion)
	r = alex.Do(t, "POST", "/api/v1/scenarios/scn-demo-sasha/publications",
		map[string]any{"draftVersion": sc.DraftVersion, "expectedActiveScenarioId": "scn-demo-sasha", "confirmReplacement": false}, h)
	if r.Status != http.StatusCreated {
		t.Fatalf("publish: %d %s", r.Status, r.Raw)
	}
	pub := events(t, env, outbox.ScenarioPublished)
	if len(pub) != 1 || pub[0].Actor != "u-alex" || pub[0].Role != "lead" || pub[0].Assignment != "assignment-sasha" ||
		pub[0].Data["revisionId"] == nil || pub[0].Data["replacedScenarioId"] != nil {
		t.Fatalf("publication event: %+v", pub)
	}

	// checklist: a repeated tick is not an event, an untick is
	sasha := env.Login(t, "u-sasha")
	var msgID, taskID string
	if err := env.DB.QueryRow(`SELECT id, block->'tasks'->0->>'id' FROM messages WHERE recipient_user_id='u-sasha' AND kind='checkpoint'
		AND jsonb_array_length(block->'tasks') > 0 ORDER BY created_at LIMIT 1`).Scan(&msgID, &taskID); err != nil {
		t.Fatal(err)
	}
	for _, v := range []bool{true, true, false} {
		if r := sasha.Do(t, "PUT", "/api/v1/messages/"+msgID+"/tasks/"+taskID+"/check", map[string]any{"checked": v}, nil); r.Status != 200 {
			t.Fatalf("check: %d %s", r.Status, r.Raw)
		}
	}
	if c, u := events(t, env, outbox.ChecklistChecked), events(t, env, outbox.ChecklistUnchecked); len(c) != 1 || len(u) != 1 ||
		c[0].Actor != "u-sasha" || c[0].Role != "intern" || c[0].Data["taskId"] != taskID {
		t.Fatalf("checklist events: %+v %+v", c, u)
	}

	// question and answer: ids and outcome only, never the text
	question := "Секретный вопрос про мою зарплату"
	r = sasha.Do(t, "POST", "/api/v1/assignments/assignment-sasha/questions", map[string]any{"text": question}, k())
	if r.Status != http.StatusAccepted {
		t.Fatalf("ask: %d %s", r.Status, r.Raw)
	}
	for i := 0; i < 5; i++ {
		if _, err := env.App.Answers.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	asked, done := events(t, env, outbox.QuestionAsked), events(t, env, outbox.AnswerCompleted)
	if len(asked) != 1 || len(done) != 1 || done[0].Data["outcome"] != "answered" || done[0].Data["citations"] != float64(1) {
		t.Fatalf("answer events: %+v %+v", asked, done)
	}
	var leak int
	if err := env.DB.QueryRow(`SELECT count(*) FROM outbox_events WHERE payload::text LIKE '%зарплат%' OR payload::text LIKE '%Ответ по материалам%'`).Scan(&leak); err != nil || leak != 0 {
		t.Fatalf("private texts must not reach events: %d %v", leak, err)
	}

	// access request: requested at confirmation, decided once by the service
	r = sasha.Do(t, "POST", "/api/v1/access-drafts", map[string]any{"assignmentId": "assignment-sasha", "resourceId": "repo-learning-read",
		"reason": "Для первой задачи"}, k())
	if r.Status != http.StatusCreated {
		t.Fatalf("draft: %d %s", r.Status, r.Raw)
	}
	r = sasha.Do(t, "POST", "/api/v1/access-drafts/"+r.JSON["id"].(string)+"/confirm",
		map[string]any{"draftVersion": 1, "reason": "Для первой задачи", "confirmed": true}, k())
	if r.Status != http.StatusCreated {
		t.Fatalf("confirm: %d %s", r.Status, r.Raw)
	}
	op := r.JSON["id"].(string)
	env.Clock.Set(env.Clock.Now().Add(apptest.MockApproveAfter + time.Second))
	for i := 0; i < 2; i++ { // two reads reconcile; the decision is recorded once
		if r := sasha.Do(t, "GET", "/api/v1/access-operations/"+op, nil, nil); r.Status != 200 || r.JSON["status"] != "approved" {
			t.Fatalf("operation: %d %s", r.Status, r.Raw)
		}
	}
	if req, dec := events(t, env, outbox.AccessRequested), events(t, env, outbox.AccessDecided); len(req) != 1 || len(dec) != 1 ||
		dec[0].Data["status"] != "approved" || dec[0].Assignment != "assignment-sasha" {
		t.Fatalf("access events: %+v %+v", req, dec)
	}

	// materials of the team: a version, then the index result (no knowledge service here: failed)
	r = alex.Do(t, "POST", "/api/v1/sources", map[string]any{"title": "Пропуск", "body": "Секретный текст памятки", "category": "Офис",
		"acl": map[string]any{"teamIds": []string{"platform"}, "allowedRoleKinds": []string{"intern"}}}, k())
	if r.Status != http.StatusAccepted {
		t.Fatalf("source: %d %s", r.Status, r.Raw)
	}
	for i := 0; i < 3; i++ {
		if _, err := env.App.Sources.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	created, failed := events(t, env, outbox.SourceVersionCreated), events(t, env, outbox.SourceIndexFailed)
	if len(created) != 1 || created[0].Team != "platform" || created[0].Actor != "u-alex" || len(failed) != 1 || failed[0].Data["errorCode"] != "INDEX_DISABLED" {
		t.Fatalf("source events: %+v %+v", created, failed)
	}
	if err := env.DB.QueryRow(`SELECT count(*) FROM outbox_events WHERE payload::text LIKE '%Секретный текст%'`).Scan(&leak); err != nil || leak != 0 {
		t.Fatal("material text must not reach events")
	}
	if strings.Contains(fmt.Sprint(created[0].Data), "Секретный") {
		t.Fatal("material text in the event")
	}
}
