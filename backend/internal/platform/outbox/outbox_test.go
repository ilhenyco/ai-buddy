package outbox_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/outbox"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/testdb"
	"github.com/ilhenyco/ai-buddy/backend/migrations"
)

func open(t *testing.T) *sql.DB {
	t.Helper()
	d := testdb.Open(t)
	if err := db.Migrate(context.Background(), d, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO users(id, display_name) VALUES ('u-i','I'),('u-b','B'),('u-l','L');
		INSERT INTO assignments(id, intern_id, buddy_id, lead_id, team_id) VALUES ('as-1','u-i','u-b','u-l','team-a')`); err != nil {
		t.Fatal(err)
	}
	return d
}

func add(t *testing.T, d *sql.DB, w *outbox.Writer, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := db.InTx(context.Background(), d, func(tx *sql.Tx) error {
			return w.Add(context.Background(), tx, outbox.Event{Type: outbox.QuestionAsked, AggregateType: "answer", AggregateID: "ans-1",
				AssignmentID: "as-1", ActorID: "u-i", ActorRole: "intern", Data: map[string]any{"answerId": "ans-1"}})
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func count(t *testing.T, d *sql.DB, where string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM outbox_events WHERE ` + where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type recorder struct {
	mu     sync.Mutex
	got    []outbox.Envelope
	fail   error
	called int
}

func (r *recorder) Publish(_ context.Context, ev []outbox.Envelope) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.called++
	if r.fail != nil {
		return r.fail
	}
	r.got = append(r.got, ev...)
	return nil
}

type observer struct {
	published map[string]int
	failed    int
	pending   int
}

func (o *observer) OutboxPublished(t string) { o.published[t]++ }
func (o *observer) OutboxFailed()            { o.failed++ }
func (o *observer) OutboxPending(n int)      { o.pending = n }

func TestEventCommitsAndRollsBackWithTheChange(t *testing.T) {
	d := open(t)
	w := outbox.NewWriter()
	add(t, d, w, 1)
	boom := errors.New("business change failed")
	err := db.InTx(context.Background(), d, func(tx *sql.Tx) error {
		if err := w.Add(context.Background(), tx, outbox.Event{Type: outbox.AnswerFailed, AggregateType: "answer", AggregateID: "ans-2"}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if count(t, d, "true") != 1 || count(t, d, "type='answer.failed'") != 0 {
		t.Fatal("a rolled back change must leave no event")
	}
	var team, actor, role string
	if err := d.QueryRow(`SELECT team_id, actor_id, actor_role FROM outbox_events`).Scan(&team, &actor, &role); err != nil || team != "team-a" || actor != "u-i" || role != "intern" {
		t.Fatalf("team from the assignment: %q %q %q %v", team, actor, role, err)
	}
	// a nil writer (EVENTS_PUBLISHER=off) records nothing and never fails
	var off *outbox.Writer
	add(t, d, off, 3)
	if count(t, d, "true") != 1 {
		t.Fatal("nil writer must not write")
	}
	if err := db.InTx(context.Background(), d, func(tx *sql.Tx) error {
		return w.Add(context.Background(), tx, outbox.Event{Type: "x"})
	}); err == nil {
		t.Fatal("an event without an aggregate must be refused")
	}
}

func TestRelayPublishesOnceAndRetriesAFailedBatch(t *testing.T) {
	d := open(t)
	add(t, d, outbox.NewWriter(), 5)
	clk := clock.NewManual(time.Now())
	rec := &recorder{fail: errors.New("collector down")}
	obs := &observer{published: map[string]int{}}
	r := &outbox.Relay{DB: d, Clock: clk, Publisher: rec, Observer: obs, Batch: 3, Backoff: time.Second,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx := context.Background()

	// the publisher fails: nothing is published, the batch waits with a growing delay
	if n, err := r.Step(ctx); err != nil || n != 3 {
		t.Fatalf("step: %d %v", n, err)
	}
	if count(t, d, "published_at IS NOT NULL") != 0 || count(t, d, "attempts=1 AND last_error='collector down'") != 3 || obs.failed != 1 || obs.pending != 5 {
		t.Fatalf("failed batch: failed=%d pending=%d", obs.failed, obs.pending)
	}
	// the other two are still due; the failed three wait for their backoff
	if n, _ := r.Step(ctx); n != 2 {
		t.Fatalf("the rest of the events: %d", n)
	}
	if n, _ := r.Step(ctx); n != 0 {
		t.Fatalf("nothing is due before the backoff: %d", n)
	}

	// the collector is back: after the delay all five are published exactly once
	rec.fail = nil
	clk.Set(clk.Now().Add(10 * time.Second))
	for i := 0; i < 3; i++ {
		if _, err := r.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(rec.got) != 5 || count(t, d, "published_at IS NULL") != 0 || obs.published[outbox.QuestionAsked] != 5 || obs.pending != 0 {
		t.Fatalf("published %d, metrics %+v", len(rec.got), obs)
	}
	e := rec.got[0]
	if e.Type != outbox.QuestionAsked || e.Version != 1 || e.Aggregate.ID != "ans-1" || *e.AssignmentID != "as-1" || *e.TeamID != "team-a" ||
		e.Actor == nil || e.Actor.UserID != "u-i" || !strings.Contains(string(e.Data), "ans-1") {
		t.Fatalf("envelope: %+v", e)
	}
	if n, _ := r.Step(ctx); n != 0 {
		t.Fatal("published events are not sent again")
	}
}

func TestParallelRelaysNeverPublishTheSameEvent(t *testing.T) {
	d := open(t)
	add(t, d, outbox.NewWriter(), 40)
	rec := &recorder{}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := &outbox.Relay{DB: d, Clock: clock.System{}, Publisher: rec, Batch: 5}
			for j := 0; j < 20; j++ {
				if _, err := r.Step(context.Background()); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, e := range rec.got {
		if seen[e.ID] {
			t.Fatalf("published twice: %s", e.ID)
		}
		seen[e.ID] = true
	}
	if len(seen) != 40 {
		t.Fatalf("published %d of 40", len(seen))
	}
}

func TestRunLoopStopsWithContext(t *testing.T) {
	d := open(t)
	add(t, d, outbox.NewWriter(), 2)
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		(&outbox.Relay{DB: d, Clock: clock.System{}, Publisher: rec}).Run(ctx, 10*time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for count(t, d, "published_at IS NULL") != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if count(t, d, "published_at IS NULL") != 0 {
		t.Fatal("the run loop must publish pending events")
	}
}

func TestHTTPPublisher(t *testing.T) {
	var body []byte
	var auth, batch string
	status := http.StatusNoContent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		auth, batch = r.Header.Get("Authorization"), r.Header.Get("X-Batch-Id")
		w.WriteHeader(status)
	}))
	defer srv.Close()
	p := outbox.HTTPPublisher{URL: srv.URL, Token: "tok"}
	ev := []outbox.Envelope{{ID: "oev-1", Type: outbox.StageDelivered, Version: 1, Aggregate: outbox.Aggregate{Type: "stage", ID: "evt-1"},
		Data: json.RawMessage(`{"blockId":"start"}`)}}
	if err := p.Publish(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	var got struct{ Events []outbox.Envelope }
	if err := json.Unmarshal(body, &got); err != nil || len(got.Events) != 1 || got.Events[0].ID != "oev-1" || auth != "Bearer tok" || batch != "oev-1" {
		t.Fatalf("sent %s, auth %q, batch %q", body, auth, batch)
	}
	status = http.StatusServiceUnavailable
	if err := p.Publish(context.Background(), ev); err == nil {
		t.Fatal("a non-2xx answer must be an error")
	}
	if err := (outbox.HTTPPublisher{URL: "http://127.0.0.1:1"}).Publish(context.Background(), ev); err == nil {
		t.Fatal("an unreachable collector must be an error")
	}
}

func TestLogPublisher(t *testing.T) {
	var buf bytes.Buffer
	uid, team := "as-1", "team-a"
	p := outbox.LogPublisher{Log: slog.New(slog.NewJSONHandler(&buf, nil))}
	err := p.Publish(context.Background(), []outbox.Envelope{{ID: "oev-1", Type: outbox.AccessDecided, AssignmentID: &uid, TeamID: &team,
		Actor: &outbox.Actor{UserID: "u-i", Role: "intern"}, Aggregate: outbox.Aggregate{Type: "access_operation", ID: "op-1"}, Data: json.RawMessage(`{}`)}})
	if err != nil || !strings.Contains(buf.String(), `"type":"access.decided"`) || !strings.Contains(buf.String(), `"teamId":"team-a"`) {
		t.Fatalf("log: %s %v", buf.String(), err)
	}
}
