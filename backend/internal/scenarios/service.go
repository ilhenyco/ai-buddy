package scenarios

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/identity"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/idempotency"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/outbox"
)

// Service implements the scenario operations on PostgreSQL.
type Service struct {
	DB    *sql.DB
	Clock clock.Clock
	// Loc is the calendar of the assignment, used for the replacement cutoff date.
	Loc *time.Location
	// AfterPublish is called after a successful publication (wakes the messages scheduler).
	AfterPublish func()
	Events       *outbox.Writer // nil: no business events
}

// Result is a ready HTTP answer; Body is JSON. ETag is set for single-scenario answers.
type Result struct {
	Status int
	Body   []byte
	ETag   string
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC().Truncate(time.Microsecond) }

// ETag of a scenario draft. If-Match must repeat it.
func ETag(version int) string { return `"` + strconv.Itoa(version) + `"` }

// ParseIfMatch accepts `"3"`, `W/"3"` or `3`; a wildcard is not allowed.
func ParseIfMatch(h string) (int, error) {
	v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(h), "W/"))
	v = strings.Trim(v, `"`)
	n, err := strconv.Atoi(v)
	if h == "" || err != nil || n < 1 {
		return 0, httpx.Validation("Нужен заголовок If-Match с версией черновика (ETag).", "If-Match")
	}
	return n, nil
}

func canon(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func equalPlans(a, b Plan) bool { return bytes.Equal(canon(a), canon(b)) }

// ---------- reading ----------

type row struct {
	id           string
	owner        string
	assignmentID *string
	draft        Plan
	version      int
	latestRev    *string
	updatedAt    time.Time
}

func loadRow(ctx context.Context, q identity.Querier, id, owner string, lock bool) (row, error) {
	query := `SELECT id, owner_id, assignment_id, draft, draft_version, latest_revision_id, updated_at
		FROM scenarios WHERE id=$1 AND owner_id=$2`
	if lock {
		query += ` FOR UPDATE`
	}
	var r row
	var draft []byte
	err := q.QueryRowContext(ctx, query, id, owner).Scan(&r.id, &r.owner, &r.assignmentID, &draft, &r.version, &r.latestRev, &r.updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, httpx.NotFound() // also for scenarios of other leads
	}
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(draft, &r.draft)
}

func loadRevision(ctx context.Context, q identity.Querier, id *string) (*Revision, error) {
	if id == nil {
		return nil, nil
	}
	rev := &Revision{ID: *id}
	var plan []byte
	if err := q.QueryRowContext(ctx, `SELECT number, published_at, plan FROM scenario_revisions WHERE id=$1`, *id).
		Scan(&rev.Number, &rev.PublishedAt, &plan); err != nil {
		return nil, err
	}
	rev.PublishedAt = rev.PublishedAt.UTC()
	return rev, json.Unmarshal(plan, &rev.Plan)
}

func activeScenarioID(ctx context.Context, q identity.Querier, assignmentID *string) (*string, error) {
	if assignmentID == nil {
		return nil, nil
	}
	var id string
	err := q.QueryRowContext(ctx, `SELECT scenario_id FROM active_scenarios WHERE assignment_id=$1`, *assignmentID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &id, err
}

func (r row) toAPI(rev *Revision, activeID *string) Scenario {
	return Scenario{
		ID: r.id, AssignmentID: r.assignmentID, Draft: r.draft, DraftVersion: r.version,
		Published: rev, IsActive: activeID != nil && *activeID == r.id,
		HasUnpublishedChanges: rev == nil || !equalPlans(r.draft, rev.Plan),
		UpdatedAt:             r.updatedAt.UTC(),
	}
}

func (s *Service) scenario(ctx context.Context, q identity.Querier, r row) (Scenario, error) {
	rev, err := loadRevision(ctx, q, r.latestRev)
	if err != nil {
		return Scenario{}, err
	}
	active, err := activeScenarioID(ctx, q, r.assignmentID)
	if err != nil {
		return Scenario{}, err
	}
	return r.toAPI(rev, active), nil
}

func requireLead(ctx context.Context, q identity.Querier, subject string) error {
	ok, err := identity.IsLead(ctx, q, subject)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Forbidden("Сценарии доступны только руководителю.")
	}
	return nil
}

func jsonResult(status int, v any, etag string) (Result, error) {
	b, err := json.Marshal(v)
	return Result{Status: status, Body: b, ETag: etag}, err
}

// ListTemplates — GET /scenario-templates.
func (s *Service) ListTemplates() TemplatePage { return TemplatePage{Items: Templates()} }

// Get — GET /scenarios/{id}; only the owner sees the scenario.
func (s *Service) Get(ctx context.Context, subject, id string) (Result, error) {
	r, err := loadRow(ctx, s.DB, id, subject, false)
	if err != nil {
		return Result{}, err
	}
	sc, err := s.scenario(ctx, s.DB, r)
	if err != nil {
		return Result{}, err
	}
	return jsonResult(200, sc, ETag(sc.DraftVersion))
}

// ListQuery mirrors the query parameters of listScenarios.
type ListQuery struct {
	Q      string
	Filter string
	Cursor string
	Limit  int
}

// List — GET /scenarios. Volumes are small (one lead), so filtering is done in memory.
func (s *Service) List(ctx context.Context, subject string, lq ListQuery) (ScenarioPage, error) {
	if err := requireLead(ctx, s.DB, subject); err != nil {
		return ScenarioPage{}, err
	}
	if lq.Filter == "" {
		lq.Filter = "all"
	}
	if lq.Filter != "all" && lq.Filter != "drafts" && lq.Filter != "published" {
		return ScenarioPage{}, httpx.Validation("Неизвестный фильтр.", "filter")
	}
	if lq.Limit == 0 {
		lq.Limit = 30
	}
	if lq.Limit < 1 || lq.Limit > 100 {
		return ScenarioPage{}, httpx.Validation("limit должен быть от 1 до 100.", "limit")
	}
	offset := 0
	if lq.Cursor != "" {
		n, err := strconv.Atoi(lq.Cursor)
		if err != nil || n < 0 {
			return ScenarioPage{}, httpx.Validation("Некорректный cursor.", "cursor")
		}
		offset = n
	}

	rows, err := s.DB.QueryContext(ctx, `SELECT s.id, s.owner_id, s.assignment_id, s.draft, s.draft_version,
			s.latest_revision_id, s.updated_at, COALESCE(u.display_name, '')
		FROM scenarios s
		LEFT JOIN assignments a ON a.id = s.assignment_id
		LEFT JOIN users u ON u.id = a.intern_id
		WHERE s.owner_id=$1`, subject)
	if err != nil {
		return ScenarioPage{}, err
	}
	type item struct {
		r      row
		intern string
	}
	var all []item
	for rows.Next() {
		var it item
		var draft []byte
		if err := rows.Scan(&it.r.id, &it.r.owner, &it.r.assignmentID, &draft, &it.r.version, &it.r.latestRev,
			&it.r.updatedAt, &it.intern); err != nil {
			rows.Close()
			return ScenarioPage{}, err
		}
		if err := json.Unmarshal(draft, &it.r.draft); err != nil {
			rows.Close()
			return ScenarioPage{}, err
		}
		all = append(all, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ScenarioPage{}, err
	}

	needle := strings.ToLower(strings.TrimSpace(lq.Q))
	page := ScenarioPage{Items: []Scenario{}}
	var matched []Scenario
	for _, it := range all {
		if needle != "" && !strings.Contains(strings.ToLower(it.r.draft.Name+" "+it.intern), needle) {
			continue
		}
		sc, err := s.scenario(ctx, s.DB, it.r)
		if err != nil {
			return ScenarioPage{}, err
		}
		page.Counts.All++
		if sc.HasUnpublishedChanges {
			page.Counts.Drafts++
		}
		if sc.Published != nil {
			page.Counts.Published++
		}
		switch {
		case lq.Filter == "drafts" && !sc.HasUnpublishedChanges, lq.Filter == "published" && sc.Published == nil:
			continue
		}
		matched = append(matched, sc)
	}
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].UpdatedAt.Equal(matched[j].UpdatedAt) {
			return matched[i].UpdatedAt.After(matched[j].UpdatedAt)
		}
		return matched[i].ID < matched[j].ID
	})
	if offset < len(matched) {
		end := min(offset+lq.Limit, len(matched))
		page.Items = matched[offset:end]
		if end < len(matched) {
			next := strconv.Itoa(end)
			page.NextCursor = &next
		}
	}
	return page, nil
}

// ---------- writing ----------

func defaultTeam(ctx context.Context, q identity.Querier, lead string) (string, error) {
	var team string
	err := q.QueryRowContext(ctx, `SELECT team_id FROM assignments WHERE lead_id=$1 ORDER BY id LIMIT 1`, lead).Scan(&team)
	if errors.Is(err, sql.ErrNoRows) {
		return "default", nil
	}
	return team, err
}

// bindPlan: participants come from the directory, never from the client.
func bindPlan(p *Plan, a *identity.Assignment, lead, team string) {
	if a != nil {
		intern, buddy, l := a.Intern.ID, a.Buddy.ID, a.Lead.ID
		p.InternID, p.BuddyID, p.LeadID, p.TeamID = &intern, &buddy, &l, a.TeamID
		return
	}
	leadID := lead
	p.InternID, p.BuddyID, p.LeadID = nil, nil, &leadID
	if strings.TrimSpace(p.TeamID) == "" {
		p.TeamID = team
	}
}

func replayResult(rp *idempotency.Replay, scenarioField bool) Result {
	var probe struct {
		DraftVersion int `json:"draftVersion"`
		Scenario     struct {
			DraftVersion int `json:"draftVersion"`
		} `json:"scenario"`
	}
	_ = json.Unmarshal(rp.Body, &probe)
	v := probe.DraftVersion
	if scenarioField {
		v = probe.Scenario.DraftVersion
	}
	return Result{Status: rp.Status, Body: rp.Body, ETag: ETag(v)}
}

// Create — POST /scenarios: a new (incomplete) draft from the 6/9-month template.
func (s *Service) Create(ctx context.Context, subject, key string, in CreateScenario) (Result, error) {
	if in.Months != 6 && in.Months != 9 {
		return Result{}, httpx.Validation("Срок стажировки — 6 или 9 месяцев.", "months")
	}
	if !validDate(in.Start) {
		return Result{}, httpx.Validation("Укажите дату старта.", "start")
	}
	if tooLong(in.Name, maxNameRunes) {
		return Result{}, httpx.Validation("Слишком длинное название.", "name")
	}
	if err := requireLead(ctx, s.DB, subject); err != nil {
		return Result{}, err
	}
	hash, err := idempotency.Hash(in)
	if err != nil {
		return Result{}, err
	}
	var res Result
	err = db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		rp, err := idempotency.Begin(ctx, tx, subject, "createScenario", key, hash)
		if err != nil || rp != nil {
			if rp != nil {
				res = replayResult(rp, false)
			}
			return err
		}
		var a *identity.Assignment
		if in.AssignmentID != nil {
			found, err := identity.LeadAssignment(ctx, tx, subject, *in.AssignmentID)
			if err != nil {
				return err
			}
			a = &found
		}
		plan, err := FromTemplate(in.Months, in.Start)
		if err != nil {
			return err
		}
		plan.Name = in.Name
		team, err := defaultTeam(ctx, tx, subject)
		if err != nil {
			return err
		}
		bindPlan(&plan, a, subject, team)
		r := row{id: newID("scn-"), owner: subject, draft: plan, version: 1, updatedAt: s.now()}
		if a != nil {
			r.assignmentID = &a.ID
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO scenarios(id, owner_id, assignment_id, draft, draft_version, created_at, updated_at)
			VALUES ($1,$2,$3,$4,1,$5,$5)`, r.id, subject, r.assignmentID, canon(plan), r.updatedAt); err != nil {
			return err
		}
		sc, err := s.scenario(ctx, tx, r)
		if err != nil {
			return err
		}
		body, err := idempotency.Save(ctx, tx, subject, "createScenario", key, hash, 201, sc)
		res = Result{Status: 201, Body: body, ETag: ETag(sc.DraftVersion)}
		return err
	})
	return res, err
}

// SaveDraft — PUT /scenarios/{id}/draft. Incomplete drafts are fine; a stale If-Match gives 412.
// Repeating the same content is idempotent even with an old If-Match (lost response).
func (s *Service) SaveDraft(ctx context.Context, subject, id string, ifMatch int, plan Plan) (Result, error) {
	if err := ValidateStructure(plan); err != nil {
		return Result{}, err
	}
	var res Result
	err := db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		r, err := loadRow(ctx, tx, id, subject, true)
		if err != nil {
			return err
		}
		var a *identity.Assignment
		if plan.InternID != nil {
			found, err := identity.LeadAssignmentByIntern(ctx, tx, subject, *plan.InternID)
			if err != nil {
				return httpx.Validation("Этот стажёр вам не назначен.", "internId")
			}
			a = &found
		}
		var newAssignment *string
		if a != nil {
			newAssignment = &a.ID
		}
		if r.latestRev != nil && !sameID(r.assignmentID, newAssignment) {
			return httpx.Conflict("PUBLISHED_INTERN_LOCKED",
				"У опубликованного сценария нельзя сменить стажёра. Создайте новый сценарий.", nil)
		}
		team, err := defaultTeam(ctx, tx, subject)
		if err != nil {
			return err
		}
		bindPlan(&plan, a, subject, team)

		if equalPlans(plan, r.draft) && sameID(r.assignmentID, newAssignment) {
			sc, err := s.scenario(ctx, tx, r) // nothing changed: idempotent PUT
			if err != nil {
				return err
			}
			res, err = jsonResult(200, sc, ETag(sc.DraftVersion))
			return err
		}
		if ifMatch != r.version {
			return httpx.VersionConflict(r.version)
		}
		r.draft, r.assignmentID, r.version, r.updatedAt = plan, newAssignment, r.version+1, s.now()
		if _, err := tx.ExecContext(ctx, `UPDATE scenarios SET draft=$2, assignment_id=$3, draft_version=$4, updated_at=$5
			WHERE id=$1`, r.id, canon(plan), r.assignmentID, r.version, r.updatedAt); err != nil {
			return err
		}
		sc, err := s.scenario(ctx, tx, r)
		if err != nil {
			return err
		}
		res, err = jsonResult(200, sc, ETag(sc.DraftVersion))
		return err
	})
	return res, err
}

func sameID(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// Publish — POST /scenarios/{id}/publications. In one transaction:
// check version and completeness, compare the active variant with the expected one,
// require confirmation for a replacement, write an immutable revision and activate it.
func (s *Service) Publish(ctx context.Context, subject, id, key string, ifMatch int, in Publish) (Result, error) {
	if in.DraftVersion != ifMatch {
		return Result{}, httpx.Validation("draftVersion и If-Match должны совпадать.", "draftVersion")
	}
	hash, err := idempotency.Hash(struct {
		ID string
		In Publish
	}{id, in})
	if err != nil {
		return Result{}, err
	}
	var res Result
	err = db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		rp, err := idempotency.Begin(ctx, tx, subject, "publishScenario", key, hash)
		if err != nil || rp != nil {
			if rp != nil {
				res = replayResult(rp, true)
			}
			return err
		}
		r, err := loadRow(ctx, tx, id, subject, true)
		if err != nil {
			return err
		}
		if r.version != in.DraftVersion {
			return httpx.VersionConflict(r.version)
		}
		if err := ValidateForPublish(r.draft); err != nil {
			return err
		}
		if r.assignmentID == nil {
			return httpx.Validation("Выберите стажёра.", "internId")
		}
		assignment := *r.assignmentID

		// Serialise publications per assignment, including the first one (no row yet).
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 1))`, "active:"+assignment); err != nil {
			return err
		}
		var current *string
		var prevCutoff *time.Time
		var cur string
		var cut sql.NullTime
		err = tx.QueryRowContext(ctx, `SELECT scenario_id, cutoff_date FROM active_scenarios WHERE assignment_id=$1`, assignment).Scan(&cur, &cut)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		default:
			current = &cur
			if cut.Valid {
				prevCutoff = &cut.Time
			}
		}
		if !sameID(current, in.ExpectedActiveScenarioID) {
			return httpx.Conflict("ACTIVE_SCENARIO_CHANGED", "Действующий сценарий стажёра изменился. Обновите страницу.",
				map[string]any{"activeScenarioId": current})
		}
		replacing := current != nil && *current != r.id
		if replacing && !in.ConfirmReplacement {
			return httpx.Conflict("REPLACEMENT_NOT_CONFIRMED", "У стажёра уже есть опубликованный сценарий. Подтвердите замену.",
				map[string]any{"activeScenarioId": *current})
		}

		var number int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(number),0)+1 FROM scenario_revisions WHERE scenario_id=$1`, r.id).Scan(&number); err != nil {
			return err
		}
		now := s.now()
		rev := &Revision{ID: newID("rev-"), Number: number, PublishedAt: now, Plan: r.draft}
		if _, err := tx.ExecContext(ctx, `INSERT INTO scenario_revisions(id, scenario_id, number, plan, published_by, published_at)
			VALUES ($1,$2,$3,$4,$5,$6)`, rev.ID, r.id, number, canon(r.draft), subject, now); err != nil {
			return err
		}

		// Cutoff (scenarios.ts publishScenario): a replacement does not backfill elapsed blocks;
		// republishing the same variant keeps its cutoff; the first publication has none.
		var cutoff *string
		switch {
		case replacing:
			today := clock.Today(s.Clock, s.Loc)
			cutoff = &today
		case prevCutoff != nil:
			d := prevCutoff.Format(time.DateOnly)
			cutoff = &d
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO active_scenarios(assignment_id, scenario_id, revision_id, cutoff_date, activated_at)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (assignment_id) DO UPDATE SET scenario_id=EXCLUDED.scenario_id, revision_id=EXCLUDED.revision_id,
				cutoff_date=EXCLUDED.cutoff_date, activated_at=EXCLUDED.activated_at`,
			assignment, r.id, rev.ID, cutoff, now); err != nil {
			return err
		}
		r.latestRev, r.updatedAt = &rev.ID, now
		if _, err := tx.ExecContext(ctx, `UPDATE scenarios SET latest_revision_id=$2, updated_at=$3 WHERE id=$1`, r.id, rev.ID, now); err != nil {
			return err
		}
		ev := map[string]any{"scenarioId": r.id, "revisionId": rev.ID, "revisionNumber": number, "blocks": len(r.draft.Blocks),
			"cutoffDate": cutoff, "replacedScenarioId": nil}
		if replacing {
			ev["replacedScenarioId"] = *current
		}
		if err := s.Events.Add(ctx, tx, outbox.Event{Type: outbox.ScenarioPublished, AggregateType: "scenario", AggregateID: r.id,
			AssignmentID: assignment, ActorID: subject, ActorRole: "lead", Data: ev}); err != nil {
			return err
		}
		active := r.id
		pub := Publication{Scenario: r.toAPI(rev, &active), CutoffDate: cutoff, ActiveRevisionID: rev.ID}
		if replacing {
			pub.ReplacedScenarioID = current
		}
		body, err := idempotency.Save(ctx, tx, subject, "publishScenario", key, hash, 201, pub)
		res = Result{Status: 201, Body: body, ETag: ETag(r.version)}
		return err
	})
	if err == nil && s.AfterPublish != nil {
		s.AfterPublish()
	}
	return res, err
}

// ActivePlans is the read side for the scheduler (Бэк-1).
func (s *Service) ActivePlans(ctx context.Context) ([]ActivePlan, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.assignment_id, a.scenario_id, a.revision_id, r.number, r.plan, a.cutoff_date
		FROM active_scenarios a JOIN scenario_revisions r ON r.id = a.revision_id ORDER BY a.assignment_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActivePlan
	for rows.Next() {
		var p ActivePlan
		var plan []byte
		var cut sql.NullTime
		if err := rows.Scan(&p.AssignmentID, &p.ScenarioID, &p.RevisionID, &p.RevisionNumber, &plan, &cut); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(plan, &p.Plan); err != nil {
			return nil, err
		}
		if cut.Valid {
			d := cut.Time.Format(time.DateOnly)
			p.CutoffDate = &d
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DueBlocks: blocks of an active plan that are due on today and not cut off,
// ordered by date (dueBlocks/deliverDue in the frontend). Deduplication of already
// delivered blocks belongs to the scheduler.
func DueBlocks(p ActivePlan, today string) []Block {
	var out []Block
	for _, b := range p.Plan.Blocks {
		if b.Date == "" || b.Date > today {
			continue
		}
		if p.CutoffDate != nil && b.Date <= *p.CutoffDate {
			continue
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}
