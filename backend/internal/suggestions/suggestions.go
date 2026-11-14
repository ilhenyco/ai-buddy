// Package suggestions: three question buttons for the intern's current stage (getSuggestions).
// The pools are product copy, not configured by the manager. A triple is drawn once per stage and
// stored, so it stays the same after answers and reloads; a new stage draws a visibly new set.
package suggestions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/identity"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
)

// PoolVersion changes when the pools below change: stored triples are drawn again.
const PoolVersion = "2026-11-14"

// Pools by stage kind; the same copy as frontend/src/suggestions.ts.
var Pools = map[string][]string{
	"start":      {"Кто мой бадди?", "Кто мой руководитель?", "Как познакомиться с командой?", "С чего начать первый день?"},
	"week":       {"Как получить доступы?", "Где найти рабочие инструменты?", "Как подготовиться к первой задаче?", "Где найти вводные курсы?"},
	"adaptation": {"Как обсудить цели стажировки?", "Какие курсы пройти?", "Как взять отгул?", "Как взять отпуск?"},
	"practice":   {"Как взять отгул?", "Как взять отпуск?", "Как попросить обратную связь?", "К кому обратиться, если застрял в задаче?"},
	"midpoint":   {"Как подготовиться к экватору?", "Какие результаты собрать?", "Что обсудить с бадди?", "Как уточнить дальнейшие цели?"},
	"final-prep": {"Как подготовить презентацию?", "Какие результаты показать?", "С кем обсудить продолжение работы?", "Что обсудить с руководителем перед финалом?"},
	"final":      {"Как представить итоги?", "Как передать свои задачи?", "Как получить обратную связь?", "С кем обсудить следующие шаги?"},
	"after":      {"Кому передать материалы?", "К кому обратиться с оставшимися вопросами?", "Как оставить обратную связь?", "С кем обсудить организационные документы?"},
	"general":    {"Где найти материалы команды?", "Как связаться с бадди?", "Как обсудить рабочий вопрос?", "К кому обратиться за помощью?"},
}

type Selection struct {
	StageKey    string    `json:"stageKey"`
	StageKind   string    `json:"stageKind"`
	PoolVersion string    `json:"poolVersion"`
	Questions   []string  `json:"questions"`
	SelectedAt  time.Time `json:"selectedAt"`
}

type Service struct {
	DB       *sql.DB
	Calendar clock.Clock // the stage follows the business calendar (moves with the demo date)
	Clock    clock.Clock
	Loc      *time.Location
	Rand     func(n int) int // nil: math/rand
}

// Kind of a block: its stageKind, or the kind its id starts with (older plans), or "general".
func Kind(b scenarios.Block) string {
	if _, ok := Pools[b.StageKind]; ok && b.StageKind != "" && b.StageKind != "general" {
		return b.StageKind
	}
	kinds := make([]string, 0, len(Pools))
	for k := range Pools {
		if k != "general" {
			kinds = append(kinds, k)
		}
	}
	sort.Slice(kinds, func(i, j int) bool { return len(kinds[i]) > len(kinds[j]) }) // "final-prep" before "final"
	for _, k := range kinds {
		if b.ID == k || strings.HasPrefix(b.ID, k+"-") {
			return k
		}
	}
	return "general"
}

// Stage: the latest block that has come by today, or "prestart" before the first one.
func Stage(p scenarios.Plan, today string) (key, kind string) {
	blocks := append([]scenarios.Block(nil), p.Blocks...)
	sort.SliceStable(blocks, func(i, j int) bool { return blocks[i].Date < blocks[j].Date })
	var current *scenarios.Block
	for i := range blocks {
		if blocks[i].Date != "" && blocks[i].Date <= today {
			current = &blocks[i]
		}
	}
	if current == nil {
		return "prestart", "start"
	}
	return "block:" + current.ID, Kind(*current)
}

// Pick draws three questions of the pool; if all three repeat the previous set, one is replaced.
func Pick(kind string, previous []string, intn func(int) int) []string {
	pool := append([]string(nil), Pools[kind]...)
	for i := len(pool) - 1; i > 0; i-- {
		j := intn(i + 1)
		pool[i], pool[j] = pool[j], pool[i]
	}
	selected := pool[:3]
	same := true
	for _, q := range selected {
		if !contains(previous, q) {
			same = false
		}
	}
	if same && len(pool) > 3 {
		selected[2] = pool[3]
	}
	return selected
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Get — GET /assignments/{id}/suggestions: only the intern of the assignment.
func (s *Service) Get(ctx context.Context, subject, assignmentID string) (Selection, error) {
	var intern, buddy, lead string
	err := s.DB.QueryRowContext(ctx, `SELECT intern_id, buddy_id, lead_id FROM assignments WHERE id=$1`, assignmentID).Scan(&intern, &buddy, &lead)
	if errors.Is(err, sql.ErrNoRows) {
		return Selection{}, httpx.NotFound()
	}
	if err != nil {
		return Selection{}, err
	}
	switch subject {
	case intern:
	case buddy, lead:
		return Selection{}, httpx.Forbidden("Подсказки вопросов есть только у стажёра.")
	default:
		return Selection{}, httpx.NotFound()
	}
	var raw []byte
	err = s.DB.QueryRowContext(ctx, `SELECT r.plan FROM active_scenarios a JOIN scenario_revisions r ON r.id=a.revision_id
		WHERE a.assignment_id=$1`, assignmentID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Selection{}, httpx.Conflict("NO_ACTIVE_SCENARIO", "План стажёра ещё не опубликован.", nil)
	}
	if err != nil {
		return Selection{}, err
	}
	var plan scenarios.Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return Selection{}, err
	}
	key, kind := Stage(plan, clock.Today(s.Calendar, s.Loc))

	var out Selection
	err = db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		var questions []byte
		err := tx.QueryRowContext(ctx, `SELECT stage_key, stage_kind, pool_version, questions, selected_at
			FROM suggestion_selections WHERE assignment_id=$1 FOR UPDATE`, assignmentID).
			Scan(&out.StageKey, &out.StageKind, &out.PoolVersion, &questions, &out.SelectedAt)
		var previous []string
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		default:
			if err := json.Unmarshal(questions, &previous); err != nil {
				return err
			}
			if out.StageKey == key && out.StageKind == kind && out.PoolVersion == PoolVersion {
				out.Questions, out.SelectedAt = previous, out.SelectedAt.UTC()
				return nil // the same stage: the same triple
			}
		}
		intn := s.Rand
		if intn == nil {
			intn = rand.IntN
		}
		out = Selection{StageKey: key, StageKind: kind, PoolVersion: PoolVersion, Questions: Pick(kind, previous, intn),
			SelectedAt: s.Clock.Now().UTC().Truncate(time.Microsecond)}
		b, _ := json.Marshal(out.Questions)
		_, err = tx.ExecContext(ctx, `INSERT INTO suggestion_selections(assignment_id, stage_key, stage_kind, pool_version, questions, selected_at)
			VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (assignment_id) DO UPDATE SET stage_key=EXCLUDED.stage_key,
			stage_kind=EXCLUDED.stage_kind, pool_version=EXCLUDED.pool_version, questions=EXCLUDED.questions, selected_at=EXCLUDED.selected_at`,
			assignmentID, key, kind, PoolVersion, string(b), out.SelectedAt)
		return err
	})
	return out, err
}

func (s *Service) Routes(mux *http.ServeMux, prefix string, auth func(http.Handler) http.Handler) {
	mux.Handle("GET "+prefix+"/assignments/{assignmentId}/suggestions", auth(httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		p, _ := identity.Subject(r.Context())
		sel, err := s.Get(r.Context(), p.ID, r.PathValue("assignmentId"))
		if err != nil {
			return err
		}
		httpx.WriteJSON(w, http.StatusOK, sel)
		return nil
	})))
}
