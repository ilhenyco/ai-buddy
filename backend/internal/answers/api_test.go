package answers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/answers"
	"github.com/ilhenyco/ai-buddy/backend/internal/apptest"
	"github.com/ilhenyco/ai-buddy/backend/internal/messages"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
)

const base = "/api/v1"

var keyN atomic.Int64

func idem() map[string]string {
	return map[string]string{"Idempotency-Key": fmt.Sprintf("k-%d-%d", time.Now().UnixNano(), keyN.Add(1))}
}

// fake knowledge service: answers with fn, remembers the inputs.
type fake struct {
	fn    func(in answers.RAGInput) (answers.RAGResult, error)
	calls []answers.RAGInput
}

func (f *fake) Answer(_ context.Context, in answers.RAGInput) (answers.RAGResult, error) {
	f.calls = append(f.calls, in)
	return f.fn(in)
}

func answered(in answers.RAGInput) (answers.RAGResult, error) {
	snap := "snap-1"
	return answers.RAGResult{RequestID: in.RequestID, Status: "answered", Reply: "Доступы выдаёт бадди по заявке.", SnapshotID: &snap, RetrievalMode: "hybrid",
		Citations: []answers.RAGCitation{
			{DocumentID: "playbook-access", Revision: "r1", Title: "Доступы", Section: "Заявка", URL: "https://example.org/access"},
			{DocumentID: "playbook-access", Revision: "r1", Title: "Доступы", Section: "Заявка", URL: "https://example.org/access"}, // same fragment twice
		}}, nil
}

func setup(t *testing.T, fn func(answers.RAGInput) (answers.RAGResult, error)) (*apptest.Env, *fake) {
	t.Helper()
	env := apptest.New(t)
	if err := scenarios.SeedDemo(context.Background(), env.DB, env.Clock, time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, err := env.App.Messages.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	f := &fake{fn: fn}
	env.App.Answers.Knowledge = f
	return env, f
}

func ask(t *testing.T, c *apptest.Client, text string) apptest.Response {
	t.Helper()
	return c.Do(t, "POST", base+"/assignments/assignment-sasha/questions", map[string]string{"text": text}, idem())
}

func step(t *testing.T, env *apptest.Env) bool {
	t.Helper()
	ok, err := env.App.Answers.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func feed(t *testing.T, c *apptest.Client) []messages.Message {
	t.Helper()
	var page messages.FeedPage
	c.Do(t, "GET", base+"/assignments/assignment-sasha/messages", nil, nil).Decode(t, &page)
	return page.Items
}

func TestQuestionIsStoredThenAnsweredWithSources(t *testing.T) {
	env, f := setup(t, answered)
	sasha := env.Login(t, "u-sasha")
	h := idem()
	r := sasha.Do(t, "POST", base+"/assignments/assignment-sasha/questions", map[string]string{"text": "  Как получить доступы?  "}, h)
	if r.Status != http.StatusAccepted || r.JSON["status"] != "queued" || r.JSON["answer"] != nil {
		t.Fatalf("ask: %d %s", r.Status, r.Raw)
	}
	id := r.JSON["id"].(string)
	// the same attempt again: the same job; the same key with another text: conflict
	if again := sasha.Do(t, "POST", base+"/assignments/assignment-sasha/questions", map[string]string{"text": "  Как получить доступы?  "}, h); again.JSON["id"] != id {
		t.Fatalf("replay: %s", again.Raw)
	}
	if other := sasha.Do(t, "POST", base+"/assignments/assignment-sasha/questions", map[string]string{"text": "Другое"}, h); other.Status != http.StatusConflict {
		t.Fatalf("same key, other text: %d", other.Status)
	}
	// stored before any call: the question is already in the history
	items := feed(t, sasha)
	q := items[len(items)-1]
	if q.Kind != "user" || q.Text != "Как получить доступы?" || q.AnswerRequestID != id {
		t.Fatalf("question in feed: %+v", q)
	}
	if got := sasha.Do(t, "GET", base+"/answers/"+id, nil, nil); got.JSON["status"] != "queued" {
		t.Fatalf("before the worker: %s", got.Raw)
	}

	if !step(t, env) || step(t, env) {
		t.Fatal("one job, processed once")
	}
	in := f.calls[0]
	if in.SubjectID != "u-sasha" || in.RequestID != id || in.Query != "Как получить доступы?" || in.MessageContext == nil ||
		!strings.Contains(*in.MessageContext, "Текущий этап стажёра: Первая неделя") {
		t.Fatalf("input to the knowledge service: %+v", in)
	}
	var job answers.Job
	sasha.Do(t, "GET", base+"/answers/"+id, nil, nil).Decode(t, &job)
	if job.Status != "completed" || job.Answer == nil || job.Error != nil || job.Answer.Kind != "assistant" ||
		len(job.Answer.Citations) != 1 || job.Answer.Citations[0].URL != "https://example.org/access" || job.Answer.Citations[0].Fragment != "Заявка" {
		t.Fatalf("job: %+v", job)
	}
	items = feed(t, sasha)
	if a := items[len(items)-1]; a.Kind != "assistant" || a.ID != job.Answer.ID || items[len(items)-2].Kind != "user" {
		t.Fatalf("feed order: %+v", items[len(items)-2:])
	}

	// the chat is personal: buddy's feed and notifications stay as they were, checks are only for checkpoints
	dasha := env.Login(t, "u-dasha")
	for _, m := range feed(t, dasha) {
		if m.Kind != "checkpoint" {
			t.Fatalf("buddy sees the chat: %+v", m)
		}
	}
	var notes messages.NotificationPage
	sasha.Do(t, "GET", base+"/notifications", nil, nil).Decode(t, &notes)
	for _, n := range notes.Items {
		if n.MessageID == job.Answer.ID || n.MessageID == job.QuestionMessageID {
			t.Fatal("a chat message became a notification")
		}
	}
	if r := sasha.Do(t, "PUT", base+"/messages/"+job.Answer.ID+"/tasks/t1/check", map[string]any{"checked": true}, nil); r.Status != http.StatusNotFound {
		t.Fatalf("check on a chat message: %d", r.Status)
	}
}

func TestOnlyTheInternAsksAndReads(t *testing.T) {
	env, _ := setup(t, answered)
	sasha := env.Login(t, "u-sasha")
	id := ask(t, sasha, "Вопрос").JSON["id"].(string)
	cases := []struct {
		who    string
		status int
	}{{"u-dasha", http.StatusForbidden}, {"u-alex", http.StatusForbidden}, {"u-oleg", http.StatusNotFound}}
	for _, c := range cases {
		cl := env.Login(t, c.who)
		if r := ask(t, cl, "Вопрос"); r.Status != c.status {
			t.Errorf("%s ask: %d", c.who, r.Status)
		}
		if r := cl.Do(t, "GET", base+"/answers/"+id, nil, nil); r.Status != http.StatusNotFound {
			t.Errorf("%s reads the answer: %d", c.who, r.Status)
		}
	}
	for _, text := range []string{"   ", strings.Repeat("я", answers.MaxQuestion+1)} {
		if r := ask(t, sasha, text); r.Status != http.StatusUnprocessableEntity {
			t.Errorf("text of %d runes: %d", len([]rune(text)), r.Status)
		}
	}
	if r := sasha.Do(t, "POST", base+"/assignments/assignment-missing/questions", map[string]string{"text": "x"}, idem()); r.Status != http.StatusNotFound {
		t.Errorf("unknown assignment: %d", r.Status)
	}
	if r := sasha.Do(t, "POST", base+"/assignments/assignment-sasha/questions", map[string]string{"text": "x"}, nil); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("no Idempotency-Key: %d", r.Status)
	}
	if r := env.Anonymous().Do(t, "GET", base+"/answers/"+id, nil, nil); r.Status != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", r.Status)
	}
}

func TestFailuresAreRetriedThenReportedHonestly(t *testing.T) {
	fails := 2
	env, f := setup(t, func(in answers.RAGInput) (answers.RAGResult, error) {
		if fails > 0 {
			fails--
			return answers.RAGResult{}, errors.New("connection refused")
		}
		return answered(in)
	})
	sasha := env.Login(t, "u-sasha")
	id := ask(t, sasha, "Где взять ноутбук?").JSON["id"].(string)
	step(t, env)
	if step(t, env) {
		t.Fatal("a retried job waits for its backoff")
	}
	env.Clock.Set(env.Clock.Now().Add(5 * time.Second))
	step(t, env)
	env.Clock.Set(env.Clock.Now().Add(10 * time.Second))
	step(t, env)
	sasha = env.Login(t, "u-sasha")
	if got := sasha.Do(t, "GET", base+"/answers/"+id, nil, nil); got.JSON["status"] != "completed" || len(f.calls) != 3 {
		t.Fatalf("after two failures: %s, calls %d", got.Raw, len(f.calls))
	}
	for _, c := range f.calls {
		if c.RequestID != id {
			t.Fatal("every attempt has the same requestId")
		}
	}

	// always down: after the last attempt the job fails, the chat gets an honest message
	f.fn = func(answers.RAGInput) (answers.RAGResult, error) { return answers.RAGResult{}, errors.New("down") }
	id = ask(t, sasha, "А столовая где?").JSON["id"].(string)
	for i := 0; i < 3; i++ {
		step(t, env)
		env.Clock.Set(env.Clock.Now().Add(time.Minute))
	}
	sasha = env.Login(t, "u-sasha")
	var job answers.Job
	sasha.Do(t, "GET", base+"/answers/"+id, nil, nil).Decode(t, &job)
	if job.Status != "failed" || job.Error == nil || job.Error.Code != "AI_UNAVAILABLE" || job.Answer == nil || job.Answer.ErrorCode != "AI_UNAVAILABLE" ||
		!strings.Contains(job.Answer.Text, "Вопрос сохранён") {
		t.Fatalf("failed job: %+v", job)
	}
	if step(t, env) {
		t.Fatal("a failed job is final")
	}
	// metrics: two retries before the first answer, two before the final failure
	mt := env.App.Metrics
	if got := [3]float64{mt.JobCount("answer", "completed"), mt.JobCount("answer", "retry"), mt.JobCount("answer", "failed")}; got != [3]float64{1, 4, 1} {
		t.Fatalf("job metrics completed/retry/failed = %v, want [1 4 1]", got)
	}
}

func TestOutcomes(t *testing.T) {
	cases := []struct {
		name, want, code string
		fn               func(answers.RAGInput) (answers.RAGResult, error)
		knowledge        bool
	}{
		{"no evidence", "В доступных материалах нет ответа", "", func(in answers.RAGInput) (answers.RAGResult, error) {
			return answers.RAGResult{RequestID: in.RequestID, Status: "no_evidence"}, nil
		}, true},
		{"conflict", "Источники расходятся", "", func(in answers.RAGInput) (answers.RAGResult, error) {
			return answers.RAGResult{RequestID: in.RequestID, Status: "conflicting_sources", Reply: "Есть две версии."}, nil
		}, true},
		{"rejected", "доступных тебе материалах", "AI_REJECTED", func(answers.RAGInput) (answers.RAGResult, error) {
			return answers.RAGResult{}, &answers.RejectedError{Status: 403}
		}, true},
		{"disabled", "не подключён", "AI_DISABLED", nil, false},
		{"unavailable inside the service", "база знаний недоступна", "AI_UNAVAILABLE", func(in answers.RAGInput) (answers.RAGResult, error) {
			return answers.RAGResult{RequestID: in.RequestID, Status: "unavailable", Reply: "Сейчас не получается"}, nil
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, _ := setup(t, c.fn)
			if !c.knowledge {
				env.App.Answers.Knowledge = nil
			}
			sasha := env.Login(t, "u-sasha")
			id := ask(t, sasha, "Вопрос").JSON["id"].(string)
			step(t, env)
			var job answers.Job
			sasha.Do(t, "GET", base+"/answers/"+id, nil, nil).Decode(t, &job)
			if job.Answer == nil || !strings.Contains(job.Answer.Text, c.want) || job.Answer.ErrorCode != c.code {
				t.Fatalf("%+v", job)
			}
			if (c.code != "") != (job.Status == "failed") {
				t.Fatalf("status %s for code %q", job.Status, c.code)
			}
			outcome := "completed"
			if c.code != "" {
				outcome = "failed"
			}
			if got := env.App.Metrics.JobCount("answer", outcome); got != 1 {
				t.Fatalf("answer jobs %s = %v, want 1", outcome, got)
			}
		})
	}
}

// A worker whose lease expired while the model was thinking must not store a second answer.
func TestLateWorkerDoesNotStoreTwice(t *testing.T) {
	var env *apptest.Env
	calls := 0
	env, _ = setup(t, func(in answers.RAGInput) (answers.RAGResult, error) {
		calls++
		if calls == 1 {
			env.Clock.Set(env.Clock.Now().Add(6 * time.Minute)) // the lease runs out
			if _, err := env.App.Answers.Step(context.Background()); err != nil {
				return answers.RAGResult{}, err
			}
			r, _ := answered(in)
			r.Reply = "поздний ответ"
			return r, nil
		}
		r, _ := answered(in)
		r.Reply = "ответ второго воркера"
		return r, nil
	})
	sasha := env.Login(t, "u-sasha")
	id := ask(t, sasha, "Вопрос").JSON["id"].(string)
	step(t, env)
	var n int
	if err := env.DB.QueryRow(`SELECT count(*) FROM messages WHERE answer_request_id=$1 AND kind='assistant'`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("answers stored: %d %v", n, err)
	}
	// the late worker did not store an answer, so it is not counted either
	if got := env.App.Metrics.JobCount("answer", "completed"); got != 1 {
		t.Fatalf("completed answer jobs = %v, want 1", got)
	}
	sasha = env.Login(t, "u-sasha")
	if got := sasha.Do(t, "GET", base+"/answers/"+id, nil, nil); got.JSON["answer"].(map[string]any)["text"] != "ответ второго воркера" {
		t.Fatalf("%s", got.Raw)
	}
}

func TestRateLimitAndRunLoop(t *testing.T) {
	env, f := setup(t, answered)
	sasha := env.Login(t, "u-sasha")
	for i := 0; i < 6; i++ {
		if r := ask(t, sasha, fmt.Sprintf("Вопрос %d", i)); r.Status != http.StatusAccepted {
			t.Fatalf("question %d: %d", i, r.Status)
		}
	}
	r := ask(t, sasha, "Ещё один")
	if r.Status != http.StatusTooManyRequests || r.Header.Get("Retry-After") != "30" || r.JSON["code"] != "RATE_LIMITED" {
		t.Fatalf("rate limit: %d %s %v", r.Status, r.Raw, r.Header)
	}
	// the background loop answers all of them without a manual step
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { env.App.Answers.Run(ctx, 20*time.Millisecond); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var pending int
		if err := env.DB.QueryRow(`SELECT count(*) FROM answer_jobs WHERE status<>'completed'`).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending: %d", pending)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if len(f.calls) != 6 {
		t.Fatalf("calls: %d", len(f.calls))
	}
	// the demo reset clears the chat too
	if r := env.Login(t, "u-alex").Do(t, "POST", base+"/demo/reset", nil, nil); r.Status != http.StatusOK {
		t.Fatalf("reset: %d %s", r.Status, r.Raw)
	}
}

func TestHTTPKnowledge(t *testing.T) {
	var status int
	var reply map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/answer" || r.Header.Get("Authorization") != "Bearer token-token-token-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var in answers.RAGInput
		_ = json.NewDecoder(r.Body).Decode(&in)
		w.WriteHeader(status)
		if reply != nil {
			reply["requestId"] = in.RequestID
			_ = json.NewEncoder(w).Encode(reply)
		}
	}))
	defer srv.Close()
	k := answers.NewHTTPKnowledge(srv.URL+"/", "token-token-token-token", time.Second)
	in := answers.RAGInput{SubjectID: "u-sasha", RequestID: "ans-1", Query: "q"}

	status, reply = 200, map[string]any{"status": "answered", "reply": "ok", "citations": []any{}, "retrievalMode": "hybrid"}
	if r, err := k.Answer(context.Background(), in); err != nil || r.Reply != "ok" {
		t.Fatalf("ok: %+v %v", r, err)
	}
	status = 403
	var rej *answers.RejectedError
	if _, err := k.Answer(context.Background(), in); !errors.As(err, &rej) || rej.Status != 403 || rej.Error() == "" {
		t.Fatalf("403: %v", err)
	}
	status, reply = 503, nil
	if _, err := k.Answer(context.Background(), in); err == nil || errors.As(err, &rej) {
		t.Fatalf("503: %v", err)
	}
	status, reply = 200, map[string]any{"status": "answered"}
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"requestId": "other", "status": "answered"})
	})
	if _, err := k.Answer(context.Background(), in); err == nil {
		t.Fatal("an answer to another request is accepted")
	}
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{")) })
	if _, err := k.Answer(context.Background(), in); err == nil {
		t.Fatal("broken JSON is accepted")
	}
	if _, err := answers.NewHTTPKnowledge("http://127.0.0.1:1", "t", time.Second).Answer(context.Background(), in); err == nil {
		t.Fatal("no server, no error")
	}
}
