package access_test

// HTTP tests on a real PostgreSQL with the access mock behind the API (apptest starts both).
// The mock decides a request apptest.MockApproveAfter after it was received.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/access"
	"github.com/ilhenyco/ai-buddy/backend/internal/apptest"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/metrics"
)

const base = "/api/v1"

var keyN atomic.Int64

func key() string { return fmt.Sprintf("k-%d-%d", time.Now().UnixNano(), keyN.Add(1)) }

func idem(k string) map[string]string { return map[string]string{"Idempotency-Key": k} }

func draft(t *testing.T, c *apptest.Client, assignment, resource string) string {
	t.Helper()
	r := c.Do(t, "POST", base+"/access-drafts", map[string]any{"assignmentId": assignment, "resourceId": resource,
		"reason": "Нужен доступ для первой задачи"}, idem(key()))
	if r.Status != http.StatusCreated {
		t.Fatalf("draft: %d %s", r.Status, r.Raw)
	}
	return r.JSON["id"].(string)
}

func confirm(t *testing.T, c *apptest.Client, draftID, k string) apptest.Response {
	t.Helper()
	return c.Do(t, "POST", base+"/access-drafts/"+draftID+"/confirm",
		map[string]any{"draftVersion": 1, "reason": "Нужен доступ для первой задачи", "confirmed": true}, idem(k))
}

func code(r apptest.Response) string {
	s, _ := r.JSON["code"].(string)
	return s
}

func TestRequestApprovedOpensProtectedTask(t *testing.T) {
	env := apptest.New(t)
	sasha := env.Login(t, "u-sasha")

	res := sasha.Do(t, "GET", base+"/access-resources", nil, nil)
	if res.Status != http.StatusOK || len(res.JSON["items"].([]any)) != len(access.Resources) {
		t.Fatalf("resources: %d %s", res.Status, res.Raw)
	}
	task := sasha.Do(t, "GET", base+"/assignments/assignment-sasha/protected-first-task", nil, nil)
	if task.Status != http.StatusForbidden || code(task) != "ACCESS_REQUIRED" {
		t.Fatalf("task before request: %d %s", task.Status, task.Raw)
	}

	d := draft(t, sasha, "assignment-sasha", "repo-learning-read")
	k := key()
	r := confirm(t, sasha, d, k)
	if r.Status != http.StatusCreated {
		t.Fatalf("confirm: %d %s", r.Status, r.Raw)
	}
	var op access.Operation
	r.Decode(t, &op)
	if op.Status != "pending" || !op.SubmissionConfirmed || op.ExternalNumber == nil || *op.ExternalNumber != "MOCK-1042" || op.CorrelationID == "" {
		t.Fatalf("operation: %+v", op)
	}

	// the same confirmation again (same key) or with a new key: the same operation, no second request
	again := confirm(t, sasha, d, k)
	other := confirm(t, sasha, d, key())
	if again.JSON["id"] != op.ID || other.JSON["id"] != op.ID || other.Status != http.StatusOK {
		t.Fatalf("repeat: %d %s / %d %s", again.Status, again.Raw, other.Status, other.Raw)
	}
	if n := env.AccessMock.Calls(); n != 1 {
		t.Fatalf("mock received %d requests, want 1", n)
	}

	// still pending: the task stays closed
	if r := sasha.Do(t, "GET", base+"/assignments/assignment-sasha/protected-first-task", nil, nil); r.Status != http.StatusForbidden {
		t.Fatalf("task while pending: %d", r.Status)
	}
	env.Clock.Set(env.Clock.Now().Add(apptest.MockApproveAfter + time.Second))

	got := sasha.Do(t, "GET", base+"/access-operations/"+op.ID, nil, nil)
	if got.Status != http.StatusOK || got.JSON["status"] != "approved" {
		t.Fatalf("status: %d %s", got.Status, got.Raw)
	}
	if n := env.App.Metrics.JobCount("access", "completed"); n != 1 {
		t.Fatalf("completed access jobs = %v, want 1", n)
	}
	task = sasha.Do(t, "GET", base+"/assignments/assignment-sasha/protected-first-task", nil, nil)
	if task.Status != http.StatusOK || task.JSON["requiredResourceId"] != "repo-learning-read" {
		t.Fatalf("task after approval: %d %s", task.Status, task.Raw)
	}

	// buddy and lead see the request, a stranger does not
	for _, u := range []string{"u-dasha", "u-alex"} {
		if r := env.Login(t, u).Do(t, "GET", base+"/access-operations/"+op.ID, nil, nil); r.Status != http.StatusOK {
			t.Fatalf("%s: %d", u, r.Status)
		}
	}
	oleg := env.Login(t, "u-oleg")
	if r := oleg.Do(t, "GET", base+"/access-operations/"+op.ID, nil, nil); r.Status != http.StatusNotFound {
		t.Fatalf("stranger: %d", r.Status)
	}
	if r := oleg.Do(t, "GET", base+"/assignments/assignment-sasha/access-operations", nil, nil); r.Status != http.StatusNotFound {
		t.Fatalf("stranger list: %d", r.Status)
	}
	list := env.Login(t, "u-dasha").Do(t, "GET", base+"/assignments/assignment-sasha/access-operations", nil, nil)
	if items := list.JSON["items"].([]any); len(items) != 1 {
		t.Fatalf("list: %s", list.Raw)
	}
}

func TestDraftAndConfirmValidation(t *testing.T) {
	env := apptest.New(t)
	sasha := env.Login(t, "u-sasha")
	cases := []struct {
		name   string
		who    *apptest.Client
		body   map[string]any
		status int
	}{
		{"buddy cannot request", env.Login(t, "u-dasha"), map[string]any{"assignmentId": "assignment-sasha", "resourceId": "repo-learning-read", "reason": "x"}, http.StatusForbidden},
		{"foreign assignment", sasha, map[string]any{"assignmentId": "assignment-kirill", "resourceId": "repo-learning-read", "reason": "x"}, http.StatusNotFound},
		{"unknown resource", sasha, map[string]any{"assignmentId": "assignment-sasha", "resourceId": "prod-db", "reason": "x"}, http.StatusUnprocessableEntity},
		{"empty reason", sasha, map[string]any{"assignmentId": "assignment-sasha", "resourceId": "repo-learning-read", "reason": "  "}, http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		if r := c.who.Do(t, "POST", base+"/access-drafts", c.body, idem(key())); r.Status != c.status {
			t.Errorf("%s: %d %s", c.name, r.Status, r.Raw)
		}
	}
	if r := sasha.Do(t, "POST", base+"/access-drafts", map[string]any{"assignmentId": "assignment-sasha"}, nil); r.Status == http.StatusCreated {
		t.Errorf("draft without Idempotency-Key accepted")
	}

	d := draft(t, sasha, "assignment-sasha", "repo-learning-read")
	k := key()
	d2 := sasha.Do(t, "POST", base+"/access-drafts", map[string]any{"assignmentId": "assignment-sasha", "resourceId": "gitlab-team", "reason": "r"}, idem(k))
	if re := sasha.Do(t, "POST", base+"/access-drafts", map[string]any{"assignmentId": "assignment-sasha", "resourceId": "gitlab-team", "reason": "r"}, idem(k)); re.JSON["id"] != d2.JSON["id"] {
		t.Errorf("draft replay: %s vs %s", re.Raw, d2.Raw)
	}

	path := base + "/access-drafts/" + d + "/confirm"
	confirms := []struct {
		name   string
		who    *apptest.Client
		body   map[string]any
		status int
	}{
		{"not confirmed", sasha, map[string]any{"draftVersion": 1, "reason": "r", "confirmed": false}, http.StatusUnprocessableEntity},
		{"no confirmation", sasha, map[string]any{"draftVersion": 1, "reason": "r"}, http.StatusUnprocessableEntity},
		{"no reason", sasha, map[string]any{"draftVersion": 1, "reason": "", "confirmed": true}, http.StatusUnprocessableEntity},
		{"stale version", sasha, map[string]any{"draftVersion": 3, "reason": "r", "confirmed": true}, http.StatusPreconditionFailed},
		{"not the author", env.Login(t, "u-dasha"), map[string]any{"draftVersion": 1, "reason": "r", "confirmed": true}, http.StatusNotFound},
	}
	for _, c := range confirms {
		if r := c.who.Do(t, "POST", path, c.body, idem(key())); r.Status != c.status {
			t.Errorf("%s: %d %s", c.name, r.Status, r.Raw)
		}
	}
	if r := sasha.Do(t, "GET", base+"/access-operations/op-missing", nil, nil); r.Status != http.StatusNotFound {
		t.Errorf("missing operation: %d", r.Status)
	}
	if env.AccessMock.Calls() != 0 {
		t.Errorf("rejected confirmations reached the service")
	}

	env.Clock.Set(env.Clock.Now().Add(25 * time.Hour))
	sasha = env.Login(t, "u-sasha") // the session lives 8 hours
	if r := confirm(t, sasha, d, key()); r.Status != http.StatusConflict || r.JSON["details"].(map[string]any)["reason"] != "DRAFT_EXPIRED" {
		t.Errorf("expired draft: %d %s", r.Status, r.Raw)
	}
}

func TestLostAnswerIsReconciledNotResubmitted(t *testing.T) {
	env := apptest.New(t)
	sasha := env.Login(t, "u-sasha")
	if r := sasha.Do(t, "POST", base+"/demo/access-mode", map[string]string{"mode": "lost"}, nil); r.Status != http.StatusOK {
		t.Fatalf("mode: %d %s", r.Status, r.Raw)
	}
	if r := sasha.Do(t, "POST", base+"/demo/access-mode", map[string]string{"mode": "maybe"}, nil); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("bad mode: %d", r.Status)
	}

	r := confirm(t, sasha, draft(t, sasha, "assignment-sasha", "repo-learning-read"), key())
	if r.Status != http.StatusCreated || r.JSON["status"] != "unknown" || r.JSON["submissionConfirmed"] != false || r.JSON["externalNumber"] != nil {
		t.Fatalf("lost answer: %d %s", r.Status, r.Raw)
	}
	id := r.JSON["id"].(string)

	sasha.Do(t, "POST", base+"/demo/access-mode", map[string]string{"mode": "approve"}, nil)
	// right after the call the outcome is not reconciled: the original call may still be running
	if got := sasha.Do(t, "GET", base+"/access-operations/"+id, nil, nil); got.JSON["status"] != "unknown" {
		t.Fatalf("reconciled too early: %s", got.Raw)
	}
	env.Clock.Set(env.Clock.Now().Add(6 * time.Second))
	got := sasha.Do(t, "GET", base+"/access-operations/"+id, nil, nil)
	if got.JSON["status"] != "pending" || got.JSON["externalNumber"] != "MOCK-1042" || got.JSON["submissionConfirmed"] != true {
		t.Fatalf("reconciled: %s", got.Raw)
	}
	if n := env.AccessMock.Calls(); n != 1 {
		t.Fatalf("mock received %d requests, want 1", n)
	}
}

func TestDeniedKeepsTaskClosed(t *testing.T) {
	env := apptest.New(t)
	kirill := env.Login(t, "u-kirill")
	kirill.Do(t, "POST", base+"/demo/access-mode", map[string]string{"mode": "deny"}, nil)
	r := confirm(t, kirill, draft(t, kirill, "assignment-kirill", "repo-learning-read"), key())
	env.Clock.Set(env.Clock.Now().Add(apptest.MockApproveAfter))
	got := kirill.Do(t, "GET", base+"/access-operations/"+r.JSON["id"].(string), nil, nil)
	if got.JSON["status"] != "denied" {
		t.Fatalf("status: %s", got.Raw)
	}
	if n := env.App.Metrics.JobCount("access", "completed"); n != 1 {
		t.Fatalf("a denied request is a finished one: completed access jobs = %v, want 1", n)
	}
	if task := kirill.Do(t, "GET", base+"/assignments/assignment-kirill/protected-first-task", nil, nil); task.Status != http.StatusForbidden {
		t.Fatalf("task: %d", task.Status)
	}
}

// fake is an access service that fails on demand.
type fake struct {
	submitErr, lookupErr error
	found                bool
	submits              []string
}

func (f *fake) Submit(_ context.Context, r access.ExternalRequest) (access.ExternalRequest, error) {
	f.submits = append(f.submits, r.CorrelationID)
	if f.submitErr != nil {
		return access.ExternalRequest{}, f.submitErr
	}
	f.found = true
	return access.ExternalRequest{CorrelationID: r.CorrelationID, Number: "EXT-1", Status: "pending"}, nil
}

func (f *fake) Lookup(_ context.Context, id string) (access.ExternalRequest, bool, error) {
	if f.lookupErr != nil {
		return access.ExternalRequest{}, false, f.lookupErr
	}
	if !f.found {
		return access.ExternalRequest{}, false, nil
	}
	return access.ExternalRequest{CorrelationID: id, Number: "EXT-1", Status: "approved"}, true, nil
}

func (f *fake) SetMode(context.Context, string) error { return errors.New("not supported") }

// The service never received the request: it is submitted again with the SAME correlationId.
func TestUnavailableServiceRetriesWithSameCorrelation(t *testing.T) {
	env := apptest.New(t)
	down := errors.New("connection refused")
	f := &fake{submitErr: down}
	mt := metrics.New()
	s := &access.Service{DB: env.DB, Clock: env.Clock, External: f, DraftTTL: time.Hour, Metrics: mt}
	ctx := context.Background()

	status, body, err := s.CreateDraft(ctx, "u-sasha", key(), access.DraftInput{AssignmentID: "assignment-sasha", ResourceID: "repo-learning-read", Reason: "r"})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("draft: %v %d", err, status)
	}
	var d access.Draft
	if err := jsonUnmarshal(body, &d); err != nil {
		t.Fatal(err)
	}
	yes := true
	_, op, err := s.ConfirmDraft(ctx, "u-sasha", d.ID, key(), access.Confirm{DraftVersion: 1, Reason: "r", Confirmed: &yes})
	if err != nil || op.Status != "unknown" {
		t.Fatalf("confirm with service down: %v %+v", err, op)
	}

	// the service is still down for lookups: the known state is kept
	f.lookupErr = down
	if op2, err := s.Get(ctx, "u-sasha", op.ID); err != nil || op2.Status != "unknown" {
		t.Fatalf("lookup down: %v %+v", err, op2)
	}
	// lookups work, submission still fails: still unknown
	f.lookupErr = nil
	if op2, err := s.Get(ctx, "u-sasha", op.ID); err != nil || op2.Status != "unknown" {
		t.Fatalf("submit down: %v %+v", err, op2)
	}
	// the service is back: the request is sent with the same correlationId
	f.submitErr = nil
	op2, err := s.Get(ctx, "u-sasha", op.ID)
	if err != nil || op2.Status != "pending" || op2.ExternalNumber == nil || *op2.ExternalNumber != "EXT-1" {
		t.Fatalf("recovered: %v %+v", err, op2)
	}
	for _, c := range f.submits {
		if c != op.CorrelationID {
			t.Fatalf("submitted with another correlationId: %v", f.submits)
		}
	}
	// final status from the service, then no more calls
	op3, _ := s.Get(ctx, "u-sasha", op.ID)
	n := len(f.submits)
	op4, _ := s.Get(ctx, "u-sasha", op.ID)
	if op3.Status != "approved" || op4.Status != "approved" || len(f.submits) != n {
		t.Fatalf("final: %+v %+v %v", op3, op4, f.submits)
	}
	if _, err := s.Get(ctx, "u-oleg", op.ID); err == nil {
		t.Fatalf("stranger sees the operation")
	}
	// metrics: an unknown outcome is a retry, not a failure (the request is reconciled later):
	// the first submission with no answer, two resubmissions (one failed again), one final decision
	if got := [3]float64{mt.JobCount("access", "failed"), mt.JobCount("access", "retry"), mt.JobCount("access", "completed")}; got != [3]float64{0, 3, 1} {
		t.Fatalf("access job metrics failed/retry/completed = %v, want [0 3 1]", got)
	}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
