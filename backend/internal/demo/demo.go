// Package demo holds the tools of the demo stand (DEMO_MODE only): the moved "today" that
// survives a restart and a reset of all demo data. None of it exists outside the demo.
//
//	POST /demo/reset  -> {"today":"…","shifted":false,"created":9}
package demo

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
)

const keyDate = "today"

type Service struct {
	DB       *sql.DB
	Calendar *clock.Offset
	Loc      *time.Location
	// Seed recreates the demo scenarios in an empty database (scenarios.SeedDemo).
	Seed func(ctx context.Context) error
	// Deliver runs the scheduler once and returns the number of new messages.
	Deliver func(ctx context.Context) (int, error)
	// AccessMode puts the access service mock back to "approve"; nil without the access module.
	AccessMode func(ctx context.Context, mode string) error
}

// SaveDate stores the moved "today" (nil: real time).
func (s *Service) SaveDate(ctx context.Context, date *string) error {
	if date == nil {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM demo_settings WHERE key=$1`, keyDate)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO demo_settings(key, value) VALUES ($1,$2)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, keyDate, *date)
	return err
}

// Restore moves the calendar to the stored date, if any (on start of the API).
func (s *Service) Restore(ctx context.Context) error {
	var date string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM demo_settings WHERE key=$1`, keyDate).Scan(&date)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.Calendar.SetToday(date, s.Loc)
}

// Reset deletes everything people did on the stand and starts the demo from scratch:
// demo scenarios again, real date, messages that are due. Users, assignments and team materials
// stay: materials are team knowledge already indexed by the knowledge service, not stand activity.
func (s *Service) Reset(ctx context.Context) (int, error) {
	err := db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `TRUNCATE suggestion_selections, answer_jobs, message_checks, messages, message_events,
			access_operations, access_drafts, active_scenarios, scenario_revisions, scenarios,
			idempotency_records, demo_settings`)
		return err
	})
	if err != nil {
		return 0, err
	}
	s.Calendar.Reset()
	if s.AccessMode != nil {
		_ = s.AccessMode(ctx, "approve") // the mock may be down; the reset itself is done
	}
	if err := s.Seed(ctx); err != nil {
		return 0, err
	}
	return s.Deliver(ctx)
}

func (s *Service) Routes(mux *http.ServeMux, prefix string, auth func(http.Handler) http.Handler) {
	mux.Handle("POST "+prefix+"/demo/reset", auth(httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		n, err := s.Reset(r.Context())
		if err != nil {
			return err
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"today": clock.Today(s.Calendar, s.Loc), "shifted": false, "created": n,
		})
		return nil
	})))
}
