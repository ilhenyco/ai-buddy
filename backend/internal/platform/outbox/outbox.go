// Package outbox records business events in the same transaction as the change that caused
// them, and a relay publishes them afterwards (at least once, with retries).
//
// Why: an event sent straight to Kafka from a handler is lost when the broker is down and is
// a lie when the transaction later rolls back. Here the event row commits or rolls back with
// the business rows; consumers drop duplicates by the event id.
//
// Privacy: events carry identifiers and outcomes only, never the text of a question, an
// answer or a material (docs/architecture/events.md).
package outbox

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Event types (docs/architecture/events.md). Version 1 for all of them.
const (
	ScenarioPublished    = "scenario.published"
	StageDelivered       = "stage.delivered"
	ChecklistChecked     = "checklist.item_checked"
	ChecklistUnchecked   = "checklist.item_unchecked"
	QuestionAsked        = "question.asked"
	AnswerCompleted      = "answer.completed"
	AnswerFailed         = "answer.failed"
	SourceVersionCreated = "source.version_created"
	SourceExcluded       = "source.excluded"
	SourceIndexed        = "source.indexed"
	SourceRemoved        = "source.removed"
	SourceIndexFailed    = "source.index_failed"
	AccessRequested      = "access.requested"
	AccessDecided        = "access.decided"
)

// Event is what a module records. AssignmentID or TeamID may be empty; with an assignment
// and no team, the team of the assignment is stored.
type Event struct {
	Type          string
	AggregateType string
	AggregateID   string
	AssignmentID  string
	TeamID        string
	ActorID       string // empty: the system (scheduler, worker)
	ActorRole     string
	OccurredAt    time.Time
	Data          map[string]any
}

// Writer adds events. A nil *Writer is valid and records nothing (EVENTS_PUBLISHER=off, tests).
type Writer struct{}

// NewWriter returns a writer that records events.
func NewWriter() *Writer { return &Writer{} }

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "oev-" + hex.EncodeToString(b)
}

// Add writes e inside the caller's transaction tx. Safe on a nil Writer.
func (w *Writer) Add(ctx context.Context, tx *sql.Tx, e Event) error {
	if w == nil {
		return nil
	}
	if e.Type == "" || e.AggregateType == "" || e.AggregateID == "" {
		return fmt.Errorf("outbox: type and aggregate are required")
	}
	if e.Data == nil {
		e.Data = map[string]any{}
	}
	data, err := json.Marshal(e.Data)
	if err != nil {
		return err
	}
	occurred := e.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_events(id, type, version, aggregate_type, aggregate_id, assignment_id, team_id,
			actor_id, actor_role, payload, occurred_at, available_at)
		VALUES ($1,$2,1,$3,$4,NULLIF($5,''),
			COALESCE(NULLIF($6,''), (SELECT team_id FROM assignments WHERE id=NULLIF($5,''))),
			NULLIF($7,''),NULLIF($8,''),$9,$10,$10)`,
		newID(), e.Type, e.AggregateType, e.AggregateID, e.AssignmentID, e.TeamID, e.ActorID, e.ActorRole, string(data), occurred.UTC())
	return err
}
