package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
	"github.com/lib/pq"
)

// Envelope is the published form of an event (docs/architecture/events.md).
type Envelope struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	Version      int             `json:"version"`
	OccurredAt   time.Time       `json:"occurredAt"`
	Aggregate    Aggregate       `json:"aggregate"`
	AssignmentID *string         `json:"assignmentId"`
	TeamID       *string         `json:"teamId"`
	Actor        *Actor          `json:"actor"`
	Data         json.RawMessage `json:"data"`
}

type Aggregate struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Actor is the user who caused the event; nil for the scheduler and background workers.
type Actor struct {
	UserID string `json:"userId"`
	Role   string `json:"role,omitempty"`
}

// Publisher delivers a batch. An error means none of the batch is taken as delivered: the
// relay retries the whole batch, so a publisher must tolerate duplicates (consumers dedupe by id).
type Publisher interface {
	Publish(ctx context.Context, events []Envelope) error
}

// Observer receives counters; *metrics.Metrics implements it. Nil: no metrics.
type Observer interface {
	OutboxPublished(eventType string)
	OutboxFailed()
	OutboxPending(n int)
}

// Relay moves pending events to the publisher.
type Relay struct {
	DB        *sql.DB
	Clock     clock.Clock // real time
	Publisher Publisher
	Log       *slog.Logger
	Observer  Observer
	Batch     int           // default 100
	Backoff   time.Duration // default 2s, doubled per failed attempt
	MaxDelay  time.Duration // default 5m
}

func (r *Relay) now() time.Time { return r.Clock.Now().UTC().Truncate(time.Microsecond) }

func (r *Relay) batch() int {
	if r.Batch <= 0 {
		return 100
	}
	return r.Batch
}

func (r *Relay) delay(attempts int) time.Duration {
	b, max := r.Backoff, r.MaxDelay
	if b <= 0 {
		b = 2 * time.Second
	}
	if max <= 0 {
		max = 5 * time.Minute
	}
	d := b
	for i := 1; i < attempts && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	return d
}

// Run publishes until ctx ends: a full batch is followed at once by the next one.
func (r *Relay) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		for {
			n, err := r.Step(ctx)
			if err != nil && ctx.Err() == nil && r.Log != nil {
				r.Log.Error("outbox relay", "err", err)
			}
			if err != nil || n < r.batch() {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Step publishes one batch of due events and returns how many it handled. Rows are locked with
// SKIP LOCKED, so several API instances never publish the same batch at the same time.
func (r *Relay) Step(ctx context.Context) (int, error) {
	n := 0
	err := db.InTx(ctx, r.DB, func(tx *sql.Tx) error {
		now := r.now()
		rows, err := tx.QueryContext(ctx, `SELECT id, type, version, occurred_at, aggregate_type, aggregate_id, assignment_id, team_id,
				actor_id, actor_role, payload, attempts
			FROM outbox_events WHERE published_at IS NULL AND available_at <= $1
			ORDER BY created_at, id LIMIT $2 FOR UPDATE SKIP LOCKED`, now, r.batch())
		if err != nil {
			return err
		}
		var events []Envelope
		var attempts []int
		for rows.Next() {
			var e Envelope
			var actor, role sql.NullString
			var a int
			if err := rows.Scan(&e.ID, &e.Type, &e.Version, &e.OccurredAt, &e.Aggregate.Type, &e.Aggregate.ID, &e.AssignmentID, &e.TeamID,
				&actor, &role, &e.Data, &a); err != nil {
				_ = rows.Close()
				return err
			}
			e.OccurredAt = e.OccurredAt.UTC()
			if actor.Valid {
				e.Actor = &Actor{UserID: actor.String, Role: role.String}
			}
			events = append(events, e)
			attempts = append(attempts, a)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		n = len(events)
		if n == 0 {
			return nil
		}
		ids := make([]string, n)
		for i, e := range events {
			ids[i] = e.ID
		}
		if perr := r.Publisher.Publish(ctx, events); perr != nil {
			if r.Observer != nil {
				r.Observer.OutboxFailed()
			}
			msg := perr.Error()
			if len(msg) > 500 {
				msg = msg[:500]
			}
			// the whole batch waits; the delay grows with the attempts of its oldest event
			_, err := tx.ExecContext(ctx, `UPDATE outbox_events SET attempts=attempts+1, last_error=$2, available_at=$3
				WHERE id = ANY($1)`, pq.Array(ids), msg, now.Add(r.delay(attempts[0]+1)))
			if err == nil && r.Log != nil {
				r.Log.Warn("outbox publish failed", "events", n, "err", perr)
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE outbox_events SET published_at=$2, attempts=attempts+1, last_error=NULL
			WHERE id = ANY($1)`, pq.Array(ids), now); err != nil {
			return err
		}
		if r.Observer != nil {
			for _, e := range events {
				r.Observer.OutboxPublished(e.Type)
			}
		}
		return nil
	})
	if err == nil && r.Observer != nil {
		var pending int
		if qerr := r.DB.QueryRowContext(ctx, `SELECT count(*) FROM outbox_events WHERE published_at IS NULL`).Scan(&pending); qerr == nil {
			r.Observer.OutboxPending(pending)
		}
	}
	return n, err
}
