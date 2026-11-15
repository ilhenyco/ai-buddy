package messages

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/outbox"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
)

// Plans is what the scheduler needs from the scenarios module.
type Plans interface {
	ActivePlans(ctx context.Context) ([]scenarios.ActivePlan, error)
}

// Service holds the scheduler and the read side of the feed.
type Service struct {
	DB    *sql.DB
	Clock clock.Clock
	Loc   *time.Location
	Plans Plans
	Log   *slog.Logger
	// SaveDemoDate stores the moved "today" so that it survives a restart (demo module); optional.
	SaveDemoDate func(ctx context.Context, date *string) error
	Events       *outbox.Writer // nil: no business events

	once sync.Once
	wake chan struct{}
}

func (s *Service) wakeup() chan struct{} {
	s.once.Do(func() { s.wake = make(chan struct{}, 1) })
	return s.wake
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC().Truncate(time.Microsecond) }

// Trigger asks the background scheduler to run soon (after a publication). Never blocks.
func (s *Service) Trigger() {
	select {
	case s.wakeup() <- struct{}{}:
	default: // a run is already pending
	}
}

// Run starts the scheduler loop: every interval and on Trigger, until ctx is done.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if n, err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
			s.Log.Error("scheduler", "err", err.Error())
		} else if n > 0 {
			s.Log.Info("scheduler", "messages", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.wakeup():
		}
	}
}

type participants struct{ intern, buddy, lead string }

func (s *Service) participants(ctx context.Context, tx *sql.Tx, assignmentID string) (participants, error) {
	var p participants
	err := tx.QueryRowContext(ctx, `SELECT intern_id, buddy_id, lead_id FROM assignments WHERE id=$1`, assignmentID).
		Scan(&p.intern, &p.buddy, &p.lead)
	return p, err
}

// hasRoleTasks: buddy and lead get a message only if some task has an action for them
// (tasksForRole in the frontend).
func hasRoleTasks(b scenarios.Block, role string) bool {
	for _, t := range b.Tasks {
		if t.Actions == nil {
			continue
		}
		if (role == "buddy" && strings.TrimSpace(t.Actions.Buddy) != "") || (role == "lead" && strings.TrimSpace(t.Actions.Lead) != "") {
			return true
		}
	}
	return false
}

func nextBlock(plan scenarios.Plan, b scenarios.Block) *NextBlock {
	blocks := append([]scenarios.Block(nil), plan.Blocks...)
	sort.SliceStable(blocks, func(i, j int) bool { return blocks[i].Date < blocks[j].Date })
	for _, n := range blocks {
		if n.Date != "" && n.Date > b.Date {
			return &NextBlock{ID: n.ID, Title: n.Title, Date: n.Date}
		}
	}
	return nil
}

// sentAt places a caught-up message at 09:00 of its block date, never in the future.
func (s *Service) sentAt(date string, now time.Time) time.Time {
	d, err := time.ParseInLocation(time.DateOnly, date, s.Loc)
	if err != nil {
		return now
	}
	at := time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, s.Loc).UTC()
	if at.After(now) {
		return now
	}
	return at
}

// RunOnce creates messages for every due block of every active plan.
// Safe to run concurrently and repeatedly: the unique event key makes it a no-op for sent blocks.
func (s *Service) RunOnce(ctx context.Context) (int, error) {
	plans, err := s.Plans.ActivePlans(ctx)
	if err != nil {
		return 0, err
	}
	today := clock.Today(s.Clock, s.Loc)
	created := 0
	for _, p := range plans {
		for _, b := range scenarios.DueBlocks(p, today) {
			n, err := s.deliver(ctx, p, b)
			if err != nil {
				return created, err
			}
			created += n
		}
	}
	return created, nil
}

func (s *Service) deliver(ctx context.Context, p scenarios.ActivePlan, b scenarios.Block) (int, error) {
	created := 0
	err := db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		now := s.now()
		eventID := newID("evt-")
		res, err := tx.ExecContext(ctx, `INSERT INTO message_events(id, assignment_id, scenario_id, revision_id, block_id, block_date, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (assignment_id, scenario_id, block_id) DO NOTHING`,
			eventID, p.AssignmentID, p.ScenarioID, p.RevisionID, b.ID, b.Date, now)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil // already sent: republication or another scheduler
		}
		// recipients are the current participants of the assignment at send time
		who, err := s.participants(ctx, tx, p.AssignmentID)
		if err != nil {
			return err
		}
		block, _ := json.Marshal(b)
		var next any // NULL when this is the last block
		if nb := nextBlock(p.Plan, b); nb != nil {
			raw, _ := json.Marshal(nb)
			next = string(raw)
		}
		at := s.sentAt(b.Date, now)
		var roles []string
		for _, r := range []struct{ role, user, text string }{
			{"intern", who.intern, b.Intern},
			{"buddy", who.buddy, b.Buddy},
			{"lead", who.lead, b.Lead},
		} {
			if r.role != "intern" && (strings.TrimSpace(r.text) == "" || !hasRoleTasks(b, r.role)) {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO messages(id, event_id, assignment_id, recipient_user_id, recipient_role,
					kind, text, block, next_block, scenario_id, revision_id, created_at)
				VALUES ($1,$2,$3,$4,$5,'checkpoint',$6,$7,$8,$9,$10,$11)`,
				newID("msg-"), eventID, p.AssignmentID, r.user, r.role, r.text, string(block), next, p.ScenarioID, p.RevisionID, at); err != nil {
				return err
			}
			roles = append(roles, r.role)
			created++
		}
		return s.Events.Add(ctx, tx, outbox.Event{Type: outbox.StageDelivered, AggregateType: "stage", AggregateID: eventID,
			AssignmentID: p.AssignmentID, Data: map[string]any{"scenarioId": p.ScenarioID, "revisionId": p.RevisionID,
				"blockId": b.ID, "blockDate": b.Date, "tasks": len(b.Tasks), "recipients": roles}})
	})
	return created, err
}
