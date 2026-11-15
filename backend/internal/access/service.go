// Package access: the intern requests a work access through the access service (OQ08 profile).
// Contract: createAccessDraft, confirmAccess, getAccess, getProtectedTask (+ list per assignment).
// The model never confirms for the user: confirmation is an explicit human action in the interface.
package access

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/idempotency"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/metrics"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/outbox"
)

type Resource struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Resources an intern can request. The first task needs read access to the learning repository.
var Resources = []Resource{
	{ID: "repo-learning-read", Title: "Репозиторий учебного проекта · чтение"},
	{ID: "gitlab-team", Title: "GitLab команды"},
	{ID: "jira-team", Title: "Доска задач команды"},
}

// FirstTask is shown only with an approved access to its resource (getProtectedTask).
var FirstTask = ProtectedTask{
	ID:                 "first-task",
	Title:              "Первая задача",
	Description:        "Исправь опечатки в README учебного проекта и открой merge request. Бадди посмотрит его вместе с тобой.",
	RequiredResourceID: "repo-learning-read",
}

type DraftInput struct {
	AssignmentID string `json:"assignmentId"`
	ResourceID   string `json:"resourceId"`
	Reason       string `json:"reason"`
}

type Draft struct {
	ID           string    `json:"id"`
	AssignmentID string    `json:"assignmentId"`
	ResourceID   string    `json:"resourceId"`
	Reason       string    `json:"reason"`
	Version      int       `json:"version"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type Confirm struct {
	DraftVersion int    `json:"draftVersion"`
	Reason       string `json:"reason"`
	Confirmed    *bool  `json:"confirmed"`
}

type Operation struct {
	ID                  string    `json:"id"`
	AssignmentID        string    `json:"assignmentId"`
	ResourceID          string    `json:"resourceId"`
	CorrelationID       string    `json:"correlationId"`
	ExternalNumber      *string   `json:"externalNumber"`
	Status              string    `json:"status"`
	SubmissionConfirmed bool      `json:"submissionConfirmed"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type ProtectedTask struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	RequiredResourceID string `json:"requiredResourceId"`
}

type Service struct {
	DB       *sql.DB
	Clock    clock.Clock
	External External
	DraftTTL time.Duration
	// InFlight: an unknown outcome younger than this is not reconciled yet, because the original
	// call may still be running (it must be longer than the client timeout).
	InFlight time.Duration
	Metrics  *metrics.Metrics // nil: no metrics
	Events   *outbox.Writer   // nil: no business events
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC().Truncate(time.Microsecond) }

func resourceKnown(id string) bool {
	for _, r := range Resources {
		if r.ID == id {
			return true
		}
	}
	return false
}

// role of the subject in the assignment: "intern", "buddy", "lead" or NOT_FOUND.
func role(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, subject, assignmentID string) (string, error) {
	var intern, buddy, lead string
	err := q.QueryRowContext(ctx, `SELECT intern_id, buddy_id, lead_id FROM assignments WHERE id=$1`, assignmentID).Scan(&intern, &buddy, &lead)
	if errors.Is(err, sql.ErrNoRows) {
		return "", httpx.NotFound()
	}
	if err != nil {
		return "", err
	}
	switch subject {
	case intern:
		return "intern", nil
	case buddy:
		return "buddy", nil
	case lead:
		return "lead", nil
	}
	return "", httpx.NotFound()
}

func validReason(r string) bool {
	r = strings.TrimSpace(r)
	return r != "" && utf8.RuneCountInString(r) <= 1000
}

// CreateDraft — POST /access-drafts. Only the intern prepares a request for own assignment.
func (s *Service) CreateDraft(ctx context.Context, subject, key string, in DraftInput) (int, []byte, error) {
	if !resourceKnown(in.ResourceID) {
		return 0, nil, httpx.Validation("Неизвестный ресурс.", "resourceId")
	}
	if !validReason(in.Reason) {
		return 0, nil, httpx.Validation("Укажите обоснование (до 1000 символов).", "reason")
	}
	r, err := role(ctx, s.DB, subject, in.AssignmentID)
	if err != nil {
		return 0, nil, err
	}
	if r != "intern" {
		return 0, nil, httpx.Forbidden("Заявку подаёт сам стажёр. Бадди и руководитель помогают с ней.")
	}
	hash, err := idempotency.Hash(in)
	if err != nil {
		return 0, nil, err
	}
	var status int
	var body []byte
	err = db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		rp, err := idempotency.Begin(ctx, tx, subject, "createAccessDraft", key, hash)
		if err != nil || rp != nil {
			if rp != nil {
				status, body = rp.Status, rp.Body
			}
			return err
		}
		now := s.now()
		d := Draft{ID: newID("ad-"), AssignmentID: in.AssignmentID, ResourceID: in.ResourceID, Reason: strings.TrimSpace(in.Reason),
			Version: 1, ExpiresAt: now.Add(s.DraftTTL)}
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_drafts(id, assignment_id, author_id, resource_id, reason, version, expires_at, created_at)
			VALUES ($1,$2,$3,$4,$5,1,$6,$7)`, d.ID, d.AssignmentID, subject, d.ResourceID, d.Reason, d.ExpiresAt, now); err != nil {
			return err
		}
		status = http.StatusCreated
		body, err = idempotency.Save(ctx, tx, subject, "createAccessDraft", key, hash, status, d)
		return err
	})
	return status, body, err
}

const opColumns = `id, assignment_id, resource_id, correlation_id, external_number, status, submission_confirmed, updated_at`

type rowScanner interface{ Scan(...any) error }

func scanOp(r rowScanner) (Operation, error) {
	var o Operation
	err := r.Scan(&o.ID, &o.AssignmentID, &o.ResourceID, &o.CorrelationID, &o.ExternalNumber, &o.Status, &o.SubmissionConfirmed, &o.UpdatedAt)
	o.UpdatedAt = o.UpdatedAt.UTC()
	return o, err
}

// ConfirmDraft — POST /access-drafts/{id}/confirm. The operation and its correlationId are
// stored first; only then the external service is called. A lost answer leaves "unknown".
func (s *Service) ConfirmDraft(ctx context.Context, subject, draftID, key string, in Confirm) (int, Operation, error) {
	if in.Confirmed == nil || !*in.Confirmed {
		return 0, Operation{}, httpx.Validation("Нужно явное подтверждение отправки.", "confirmed")
	}
	if !validReason(in.Reason) {
		return 0, Operation{}, httpx.Validation("Укажите обоснование (до 1000 символов).", "reason")
	}
	hash, err := idempotency.Hash(struct {
		ID string
		In Confirm
	}{draftID, in})
	if err != nil {
		return 0, Operation{}, err
	}
	var op Operation
	var opID, reason string
	status := http.StatusCreated
	submit := false
	err = db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		rp, err := idempotency.Begin(ctx, tx, subject, "confirmAccess", key, hash)
		if err != nil {
			return err
		}
		if rp != nil { // repeat of the same confirmation: the same operation, its current state
			var stored Operation
			if err := json.Unmarshal(rp.Body, &stored); err != nil {
				return err
			}
			opID, status = stored.ID, rp.Status
			return nil
		}
		var d struct {
			assignment, resource string
			version              int
			expires              time.Time
		}
		err = tx.QueryRowContext(ctx, `SELECT assignment_id, resource_id, version, expires_at FROM access_drafts
			WHERE id=$1 AND author_id=$2 FOR UPDATE`, draftID, subject).Scan(&d.assignment, &d.resource, &d.version, &d.expires)
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.NotFound()
		}
		if err != nil {
			return err
		}
		// one submission per draft, whatever the key
		err = tx.QueryRowContext(ctx, `SELECT id FROM access_operations WHERE draft_id=$1`, draftID).Scan(&opID)
		if err == nil {
			status = http.StatusOK
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if in.DraftVersion != d.version {
			return httpx.VersionConflict(d.version)
		}
		if s.now().After(d.expires) {
			return httpx.Conflict("DRAFT_EXPIRED", "Черновик заявки устарел. Создайте новый.", nil)
		}
		now := s.now()
		reason = strings.TrimSpace(in.Reason)
		op = Operation{ID: newID("op-"), AssignmentID: d.assignment, ResourceID: d.resource, CorrelationID: newID("corr-"),
			Status: "unknown", UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_operations(id, draft_id, assignment_id, requester_id, resource_id, reason,
				correlation_id, status, submission_confirmed, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'unknown',false,$8,$8)`,
			op.ID, draftID, op.AssignmentID, subject, op.ResourceID, reason, op.CorrelationID, now); err != nil {
			return err
		}
		if err := s.Events.Add(ctx, tx, outbox.Event{Type: outbox.AccessRequested, AggregateType: "access_operation", AggregateID: op.ID,
			AssignmentID: op.AssignmentID, ActorID: subject, ActorRole: "intern",
			Data: map[string]any{"operationId": op.ID, "resourceId": op.ResourceID, "correlationId": op.CorrelationID}}); err != nil {
			return err
		}
		opID, submit = op.ID, true
		_, err = idempotency.Save(ctx, tx, subject, "confirmAccess", key, hash, http.StatusCreated, op)
		return err
	})
	if err != nil {
		return 0, Operation{}, err
	}
	if submit {
		ext, err := s.External.Submit(ctx, ExternalRequest{CorrelationID: op.CorrelationID, UserID: subject, ResourceID: op.ResourceID, Reason: reason})
		if err == nil {
			if err := s.applyExternal(ctx, op.ID, ext); err != nil {
				return 0, Operation{}, err
			}
		} else {
			// the outcome stays "unknown" and getAccess reconciles it later (usually approved),
			// so it is a retry, not a failure: otherwise one request would be in failed and completed
			s.Metrics.Job(metrics.KindAccess, metrics.OutcomeRetry)
		}
	}
	op, err = s.load(ctx, opID)
	return status, op, err
}

func (s *Service) load(ctx context.Context, id string) (Operation, error) {
	op, err := scanOp(s.DB.QueryRowContext(ctx, `SELECT `+opColumns+` FROM access_operations WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return op, httpx.NotFound()
	}
	return op, err
}

func (s *Service) applyExternal(ctx context.Context, id string, ext ExternalRequest) error {
	status := ext.Status
	if status != "pending" && status != "approved" && status != "denied" {
		status = "unknown"
	}
	decided := false
	err := db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		// FOR UPDATE: two reconciles of the same operation record the decision once
		var prev, assignment string
		if err := tx.QueryRowContext(ctx, `SELECT status, assignment_id FROM access_operations WHERE id=$1 FOR UPDATE`, id).Scan(&prev, &assignment); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE access_operations SET external_number=$2, status=$3, submission_confirmed=true, updated_at=$4
			WHERE id=$1 AND (status<>$3 OR external_number IS DISTINCT FROM $2 OR NOT submission_confirmed)`, id, ext.Number, status, s.now())
		if err != nil {
			return err
		}
		// counted once: a repeated identical update changes no row
		if n, _ := res.RowsAffected(); n == 0 || prev == status || (status != "approved" && status != "denied") {
			return nil
		}
		decided = true
		return s.Events.Add(ctx, tx, outbox.Event{Type: outbox.AccessDecided, AggregateType: "access_operation", AggregateID: id,
			AssignmentID: assignment, Data: map[string]any{"operationId": id, "status": status}})
	})
	if err == nil && decided {
		s.Metrics.Job(metrics.KindAccess, metrics.OutcomeCompleted)
	}
	return err
}

// reconcile refreshes an operation that is not final yet. An unknown outcome is checked by
// correlationId; only when the service surely has no such request is it submitted again with
// the same correlationId, which the service deduplicates.
func (s *Service) reconcile(ctx context.Context, op Operation) (Operation, error) {
	if op.Status == "approved" || op.Status == "denied" {
		return op, nil
	}
	if op.Status == "unknown" && !op.SubmissionConfirmed && s.now().Sub(op.UpdatedAt) < s.InFlight {
		return op, nil
	}
	ext, found, err := s.External.Lookup(ctx, op.CorrelationID)
	if err != nil {
		return op, nil // service unavailable: keep the known state, try next time
	}
	if !found {
		if op.SubmissionConfirmed {
			return op, nil
		}
		var requester, reason string
		if err := s.DB.QueryRowContext(ctx, `SELECT requester_id, reason FROM access_operations WHERE id=$1`, op.ID).Scan(&requester, &reason); err != nil {
			return op, err
		}
		s.Metrics.Job(metrics.KindAccess, metrics.OutcomeRetry) // the same correlationId is submitted again
		ext, err = s.External.Submit(ctx, ExternalRequest{CorrelationID: op.CorrelationID, UserID: requester, ResourceID: op.ResourceID, Reason: reason})
		if err != nil {
			return op, nil
		}
	}
	if err := s.applyExternal(ctx, op.ID, ext); err != nil {
		return op, err
	}
	return s.load(ctx, op.ID)
}

// Get — GET /access-operations/{id}: intern, buddy and lead of the assignment see the status.
func (s *Service) Get(ctx context.Context, subject, id string) (Operation, error) {
	op, err := s.load(ctx, id)
	if err != nil {
		return op, err
	}
	if _, err := role(ctx, s.DB, subject, op.AssignmentID); err != nil {
		return Operation{}, err
	}
	return s.reconcile(ctx, op)
}

// List — GET /assignments/{id}/access-operations (extension): requests of the assignment, newest first.
func (s *Service) List(ctx context.Context, subject, assignmentID string) ([]Operation, error) {
	if _, err := role(ctx, s.DB, subject, assignmentID); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+opColumns+` FROM access_operations WHERE assignment_id=$1 ORDER BY created_at DESC, id`, assignmentID)
	if err != nil {
		return nil, err
	}
	var ops []Operation
	for rows.Next() {
		op, err := scanOp(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		ops = append(ops, op)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []Operation{}
	for _, op := range ops {
		op, err := s.reconcile(ctx, op)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, nil
}

// ProtectedTask — GET /assignments/{id}/protected-first-task: only with an approved access to the
// required resource. The checklist mark is not an entitlement. Other messages stay available.
func (s *Service) ProtectedTask(ctx context.Context, subject, assignmentID string) (ProtectedTask, error) {
	ops, err := s.List(ctx, subject, assignmentID)
	if err != nil {
		return ProtectedTask{}, err
	}
	for _, op := range ops {
		if op.ResourceID == FirstTask.RequiredResourceID && op.Status == "approved" {
			return FirstTask, nil
		}
	}
	return ProtectedTask{}, &httpx.Error{Status: http.StatusForbidden, Code: "ACCESS_REQUIRED",
		Message: "Постановка откроется после одобрения доступа к репозиторию учебного проекта.",
		Details: map[string]any{"requiredResourceId": FirstTask.RequiredResourceID}}
}
