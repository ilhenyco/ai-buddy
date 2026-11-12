package scenarios

import (
	"context"
	"database/sql"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/identity"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
)

// Demo scenarios (DEMO_MODE): created once, when the database has no scenarios yet,
// so that `docker compose up` shows a working feed right away. Dates are relative to today.
type demoScenario struct {
	id, lead, assignment, name string
	months                     int
	startOffsetDays            int  // start relative to today
	publish                    bool // published and active for the intern
	incomplete                 bool // a draft with an empty block title: saves, but cannot be published
}

var demoScenarios = []demoScenario{
	// Саша: the first two blocks (first day, first week) are already due -> messages in the feed.
	{id: "scn-demo-sasha", lead: "u-alex", assignment: "assignment-sasha", name: "Backend · Платформа", months: 6, startOffsetDays: -7, publish: true},
	// Маша: an unfinished draft for the 9-month internship that starts next week.
	{id: "scn-demo-masha", lead: "u-alex", assignment: "assignment-masha", name: "Backend · Платформа · 9 месяцев", months: 9, startOffsetDays: 7, incomplete: true},
	// Кирилл: another team and lead. Алексей must not see it (access isolation).
	{id: "scn-demo-kirill", lead: "u-irina", assignment: "assignment-kirill", name: "Mobile · стажировка", months: 6, startOffsetDays: -1, publish: true},
}

// SeedDemo inserts the demo scenarios if there are no scenarios at all. Safe to call on every start.
func SeedDemo(ctx context.Context, d *sql.DB, clk clock.Clock, loc *time.Location) error {
	var exists bool
	if err := d.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM scenarios)`).Scan(&exists); err != nil || exists {
		return err
	}
	today, err := time.Parse(time.DateOnly, clock.Today(clk, loc))
	if err != nil {
		return err
	}
	now := clk.Now().UTC().Truncate(time.Microsecond)
	return db.InTx(ctx, d, func(tx *sql.Tx) error {
		for _, s := range demoScenarios {
			a, err := identity.LeadAssignment(ctx, tx, s.lead, s.assignment)
			if err != nil {
				return err // the demo users come from seed/demo.sql
			}
			plan, err := FromTemplate(s.months, today.AddDate(0, 0, s.startOffsetDays).Format(time.DateOnly))
			if err != nil {
				return err
			}
			plan.Name = s.name
			bindPlan(&plan, &a, s.lead, a.TeamID)
			if s.incomplete {
				plan.Blocks[1].Title = ""
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO scenarios(id, owner_id, assignment_id, draft, draft_version, created_at, updated_at)
				VALUES ($1,$2,$3,$4,1,$5,$5)`, s.id, s.lead, a.ID, canon(plan), now); err != nil {
				return err
			}
			if !s.publish {
				continue
			}
			rev := s.id + "-rev-1"
			if _, err := tx.ExecContext(ctx, `INSERT INTO scenario_revisions(id, scenario_id, number, plan, published_by, published_at)
				VALUES ($1,$2,1,$3,$4,$5)`, rev, s.id, canon(plan), s.lead, now); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE scenarios SET latest_revision_id=$2 WHERE id=$1`, s.id, rev); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO active_scenarios(assignment_id, scenario_id, revision_id, cutoff_date, activated_at)
				VALUES ($1,$2,$3,NULL,$4)`, a.ID, s.id, rev, now); err != nil {
				return err
			}
		}
		return nil
	})
}
