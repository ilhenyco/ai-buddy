// Package sources: team materials Хомыч answers from (listSources, createSource, getSource,
// updateSource, excludeSource, getSourceJob).
//
// The lead of a team manages its materials; the buddy reads them. Every save is a new immutable
// version and an index job; the knowledge service indexes it in the background. Until a new
// version is indexed, answers keep using the previous one. Exclusion removes the material from
// the index; the history of versions stays.
package sources

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ilhenyco/ai-buddy/backend/internal/identity"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/idempotency"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/metrics"
)

const MaxBody = 1_000_000 // bytes, as the contract and the knowledge service accept

type ACL struct {
	TeamIDs          []string `json:"teamIds"`
	AllowedRoleKinds []string `json:"allowedRoleKinds,omitempty"`
}

type Input struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Category string `json:"category"`
	ACL      *ACL   `json:"acl"`
}

type Source struct {
	ID        string    `json:"id"`
	VersionID *string   `json:"versionId"` // the version answers use now; null before the first index
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Category  string    `json:"category"`
	ACL       ACL       `json:"acl"`
	Status    string    `json:"status"` // ready | indexing | error | excluded
	UpdatedAt time.Time `json:"updatedAt"`
	ETag      string    `json:"etag"`
}

type Page struct {
	Items      []Source `json:"items"`
	NextCursor *string  `json:"nextCursor"`
}

type JobError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}

// Job is the contract's IndexJob.
type Job struct {
	ID                 string    `json:"id"`
	SourceID           string    `json:"sourceId"`
	CandidateVersionID string    `json:"candidateVersionId"`
	Status             string    `json:"status"`
	ActiveVersionID    *string   `json:"activeVersionId"`
	Error              *JobError `json:"error"`
}

type Service struct {
	DB    *sql.DB
	Clock clock.Clock
	Index Index // nil: the knowledge service is not connected, index jobs fail with an honest code
	// URLBase + source id is the link stored with the document: the app opens such links itself.
	URLBase string
	Log     *slog.Logger
	Metrics *metrics.Metrics // nil: no metrics
	Poll    time.Duration    // how often a running index job is checked; default 2s

	once sync.Once
	wake chan struct{}
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC().Truncate(time.Microsecond) }

func (s *Service) poll() time.Duration {
	if s.Poll == 0 {
		return 2 * time.Second
	}
	return s.Poll
}

func (s *Service) wakeup() chan struct{} {
	s.once.Do(func() { s.wake = make(chan struct{}, 1) })
	return s.wake
}

func (s *Service) Trigger() {
	select {
	case s.wakeup() <- struct{}{}:
	default:
	}
}

// teams where the subject is lead (write), lead or buddy (staff: the materials page) and
// any member including the intern (read: a material an answer cites).
func (s *Service) teams(ctx context.Context, subject string) (write, staff, read map[string]bool, err error) {
	as, err := identity.Assignments(ctx, s.DB, subject)
	if err != nil {
		return nil, nil, nil, err
	}
	write, staff, read = map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, a := range as {
		read[a.TeamID] = true
		if a.Lead.ID == subject {
			write[a.TeamID], staff[a.TeamID] = true, true
		}
		if a.Buddy.ID == subject {
			staff[a.TeamID] = true
		}
	}
	return write, staff, read, nil
}

func validate(in Input, write map[string]bool) (Input, error) {
	in.Title, in.Category, in.Body = strings.TrimSpace(in.Title), strings.TrimSpace(in.Category), strings.TrimSpace(in.Body)
	switch {
	case in.Title == "" || utf8.RuneCountInString(in.Title) > 200:
		return in, httpx.Validation("Укажите название (до 200 символов).", "title")
	case in.Body == "":
		return in, httpx.Validation("Добавьте текст материала.", "body")
	case len(in.Body) > MaxBody:
		return in, httpx.Validation("Текст длиннее 1 МБ. Разделите материал на части.", "body")
	case in.Category == "" || utf8.RuneCountInString(in.Category) > 80:
		return in, httpx.Validation("Укажите раздел (до 80 символов).", "category")
	case in.ACL == nil || len(in.ACL.TeamIDs) != 1:
		// the knowledge service scopes a document to one team
		return in, httpx.Validation("Материал относится к одной команде.", "acl.teamIds")
	}
	for _, r := range in.ACL.AllowedRoleKinds {
		if r != "intern" && r != "buddy" && r != "lead" {
			return in, httpx.Validation("Роли: intern, buddy, lead.", "acl.allowedRoleKinds")
		}
	}
	if !write[in.ACL.TeamIDs[0]] {
		return in, httpx.Forbidden("Материалы команды добавляет её руководитель.")
	}
	return in, nil
}

const sourceSelect = `SELECT s.id, s.active_version_id, v.title, v.body, v.category, v.acl, s.excluded, s.etag, s.updated_at,
	(SELECT j.status FROM source_jobs j WHERE j.source_id=s.id ORDER BY j.created_at DESC, j.id DESC LIMIT 1)
	FROM sources s JOIN source_versions v ON v.id=s.latest_version_id `

type scanner interface{ Scan(...any) error }

func scanSource(r scanner) (Source, error) {
	var src Source
	var acl []byte
	var excluded bool
	var etag int
	var job sql.NullString
	if err := r.Scan(&src.ID, &src.VersionID, &src.Title, &src.Body, &src.Category, &acl, &excluded, &etag, &src.UpdatedAt, &job); err != nil {
		return src, err
	}
	src.UpdatedAt, src.ETag = src.UpdatedAt.UTC(), strconv.Itoa(etag)
	if err := json.Unmarshal(acl, &src.ACL); err != nil {
		return src, err
	}
	switch {
	case excluded:
		src.Status = "excluded"
	case job.String == "failed":
		src.Status = "error"
	case job.String == "ready":
		src.Status = "ready"
	default:
		src.Status = "indexing"
	}
	return src, nil
}

// List — GET /sources: materials of the subject's teams, newest first; q filters by title.
func (s *Service) List(ctx context.Context, subject, q, cursor string, limit int) (Page, error) {
	_, read, _, err := s.teams(ctx, subject)
	if err != nil {
		return Page{}, err
	}
	if len(read) == 0 {
		return Page{}, httpx.Forbidden("Материалы доступны руководителю и бадди команды.")
	}
	offset := 0
	if cursor != "" {
		if offset, err = strconv.Atoi(cursor); err != nil || offset < 0 {
			return Page{}, httpx.Validation("Неверный cursor.", "cursor")
		}
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	teams := make([]string, 0, len(read))
	for t := range read {
		teams = append(teams, t)
	}
	tj, _ := json.Marshal(teams)
	rows, err := s.DB.QueryContext(ctx, sourceSelect+`WHERE s.team_id IN (SELECT jsonb_array_elements_text($1::jsonb))
		AND ($2 = '' OR v.title ILIKE '%' || $2 || '%') ORDER BY s.updated_at DESC, s.id OFFSET $3 LIMIT $4`,
		string(tj), strings.TrimSpace(q), offset, limit+1)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	out := Page{Items: []Source{}}
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return Page{}, err
		}
		out.Items = append(out.Items, src)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		next := strconv.Itoa(offset + limit)
		out.NextCursor = &next
	}
	return out, rows.Err()
}

// load reads a source the subject may read (write=true: may change); otherwise NOT_FOUND / FORBIDDEN.
func (s *Service) load(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, subject, id string, write bool) (Source, string, error) {
	w, staff, member, err := s.teams(ctx, subject)
	if err != nil {
		return Source{}, "", err
	}
	var team string
	var excluded bool
	err = q.QueryRowContext(ctx, `SELECT team_id, excluded FROM sources WHERE id=$1`, id).Scan(&team, &excluded)
	// the intern reads only materials of own team that answers may cite (not excluded)
	visible := err == nil && (staff[team] || (member[team] && !excluded))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !visible) {
		return Source{}, "", httpx.NotFound()
	} else if err != nil {
		return Source{}, "", err
	}
	if write && !w[team] {
		return Source{}, "", httpx.Forbidden("Материалы команды меняет её руководитель.")
	}
	src, err := scanSource(q.QueryRowContext(ctx, sourceSelect+`WHERE s.id=$1`, id))
	return src, team, err
}

// Get — GET /sources/{id}?versionId=: the latest version, or the given one (history).
func (s *Service) Get(ctx context.Context, subject, id, versionID string) (Source, error) {
	src, _, err := s.load(ctx, s.DB, subject, id, false)
	if err != nil || versionID == "" {
		return src, err
	}
	var acl []byte
	err = s.DB.QueryRowContext(ctx, `SELECT title, body, category, acl FROM source_versions WHERE id=$1 AND source_id=$2`, versionID, id).
		Scan(&src.Title, &src.Body, &src.Category, &acl)
	if errors.Is(err, sql.ErrNoRows) {
		return src, httpx.NotFound()
	}
	if err != nil {
		return src, err
	}
	return src, json.Unmarshal(acl, &src.ACL)
}

func (s *Service) insertVersion(ctx context.Context, tx *sql.Tx, sourceID, subject string, in Input, now time.Time) (string, error) {
	id := newID("sv-")
	acl, _ := json.Marshal(in.ACL)
	_, err := tx.ExecContext(ctx, `INSERT INTO source_versions(id, source_id, title, body, category, acl, author_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, id, sourceID, in.Title, in.Body, in.Category, string(acl), subject, now)
	return id, err
}

func (s *Service) insertJob(ctx context.Context, tx *sql.Tx, sourceID, versionID, action string, now time.Time) (Job, error) {
	j := Job{ID: newID("idx-"), SourceID: sourceID, CandidateVersionID: versionID, Status: "queued"}
	if _, err := tx.ExecContext(ctx, `INSERT INTO source_jobs(id, source_id, candidate_version_id, action, status, available_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,'queued',$5,$5,$5)`, j.ID, sourceID, versionID, action, now); err != nil {
		return j, err
	}
	return j, tx.QueryRowContext(ctx, `SELECT active_version_id FROM sources WHERE id=$1`, sourceID).Scan(&j.ActiveVersionID)
}

// Create — POST /sources: 202 with the index job.
func (s *Service) Create(ctx context.Context, subject, key string, in Input) (int, []byte, error) {
	write, _, _, err := s.teams(ctx, subject)
	if err != nil {
		return 0, nil, err
	}
	if in, err = validate(in, write); err != nil {
		return 0, nil, err
	}
	hash, err := idempotency.Hash(in)
	if err != nil {
		return 0, nil, err
	}
	var status int
	var body []byte
	err = db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		rp, err := idempotency.Begin(ctx, tx, subject, "createSource", key, hash)
		if err != nil || rp != nil {
			if rp != nil {
				status, body = rp.Status, rp.Body
			}
			return err
		}
		now, id := s.now(), newID("src-")
		if _, err := tx.ExecContext(ctx, `INSERT INTO sources(id, team_id, category, title, latest_version_id, created_by, created_at, updated_at)
			VALUES ($1,$2,$3,$4,'',$5,$6,$6)`, id, in.ACL.TeamIDs[0], in.Category, in.Title, subject, now); err != nil {
			return err
		}
		v, err := s.insertVersion(ctx, tx, id, subject, in, now)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sources SET latest_version_id=$2 WHERE id=$1`, id, v); err != nil {
			return err
		}
		j, err := s.insertJob(ctx, tx, id, v, "index", now)
		if err != nil {
			return err
		}
		status = 202
		body, err = idempotency.Save(ctx, tx, subject, "createSource", key, hash, status, j)
		return err
	})
	if err == nil {
		s.Trigger()
	}
	return status, body, err
}

// Update — PUT /sources/{id} with If-Match: a new version and its index job.
func (s *Service) Update(ctx context.Context, subject, id, key string, etag int, in Input) (int, []byte, error) {
	write, _, _, err := s.teams(ctx, subject)
	if err != nil {
		return 0, nil, err
	}
	hash, err := idempotency.Hash(struct {
		ID   string
		ETag int
		In   Input
	}{id, etag, in})
	if err != nil {
		return 0, nil, err
	}
	var status int
	var body []byte
	err = db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		rp, err := idempotency.Begin(ctx, tx, subject, "updateSource", key, hash)
		if err != nil || rp != nil {
			if rp != nil {
				status, body = rp.Status, rp.Body
			}
			return err
		}
		if _, _, err := s.load(ctx, tx, subject, id, true); err != nil {
			return err
		}
		if in, err = validate(in, write); err != nil {
			return err
		}
		var current int
		var excluded bool
		if err := tx.QueryRowContext(ctx, `SELECT etag, excluded FROM sources WHERE id=$1 FOR UPDATE`, id).Scan(&current, &excluded); err != nil {
			return err
		}
		if current != etag {
			return &httpx.Error{Status: 412, Code: httpx.CodeVersionConflict, Message: "Материал изменён в другом месте. Обновите страницу.",
				Details: map[string]any{"currentVersion": current}}
		}
		now := s.now()
		v, err := s.insertVersion(ctx, tx, id, subject, in, now)
		if err != nil {
			return err
		}
		// a new version brings an excluded material back
		if _, err := tx.ExecContext(ctx, `UPDATE sources SET latest_version_id=$2, team_id=$3, category=$4, title=$5, excluded=false,
			etag=etag+1, updated_at=$6 WHERE id=$1`, id, v, in.ACL.TeamIDs[0], in.Category, in.Title, now); err != nil {
			return err
		}
		j, err := s.insertJob(ctx, tx, id, v, "index", now)
		if err != nil {
			return err
		}
		status = 202
		body, err = idempotency.Save(ctx, tx, subject, "updateSource", key, hash, status, j)
		return err
	})
	if err == nil {
		s.Trigger()
	}
	return status, body, err
}

// Exclude — POST /sources/{id}/exclude with If-Match: out of new answers, versions stay.
func (s *Service) Exclude(ctx context.Context, subject, id, key string, etag int) (int, []byte, error) {
	hash, err := idempotency.Hash(struct {
		ID   string
		ETag int
	}{id, etag})
	if err != nil {
		return 0, nil, err
	}
	var status int
	var body []byte
	err = db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		rp, err := idempotency.Begin(ctx, tx, subject, "excludeSource", key, hash)
		if err != nil || rp != nil {
			if rp != nil {
				status, body = rp.Status, rp.Body
			}
			return err
		}
		if _, _, err := s.load(ctx, tx, subject, id, true); err != nil {
			return err
		}
		var current int
		var latest string
		var excluded bool
		if err := tx.QueryRowContext(ctx, `SELECT etag, latest_version_id, excluded FROM sources WHERE id=$1 FOR UPDATE`, id).Scan(&current, &latest, &excluded); err != nil {
			return err
		}
		if current != etag {
			return &httpx.Error{Status: 412, Code: httpx.CodeVersionConflict, Message: "Материал изменён в другом месте. Обновите страницу.",
				Details: map[string]any{"currentVersion": current}}
		}
		now := s.now()
		if !excluded {
			if _, err := tx.ExecContext(ctx, `UPDATE sources SET excluded=true, etag=etag+1, updated_at=$2 WHERE id=$1`, id, now); err != nil {
				return err
			}
			if _, err := s.insertJob(ctx, tx, id, latest, "remove", now); err != nil {
				return err
			}
		}
		src, err := scanSource(tx.QueryRowContext(ctx, sourceSelect+`WHERE s.id=$1`, id))
		if err != nil {
			return err
		}
		status = 200
		body, err = idempotency.Save(ctx, tx, subject, "excludeSource", key, hash, status, src)
		return err
	})
	if err == nil {
		s.Trigger()
	}
	return status, body, err
}

// GetJob — GET /source-jobs/{id}: for readers of the source's team.
func (s *Service) GetJob(ctx context.Context, subject, id string) (Job, error) {
	var j Job
	var errCode sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT j.id, j.source_id, j.candidate_version_id, j.status, s.active_version_id, j.error_code
		FROM source_jobs j JOIN sources s ON s.id=j.source_id WHERE j.id=$1`, id).
		Scan(&j.ID, &j.SourceID, &j.CandidateVersionID, &j.Status, &j.ActiveVersionID, &errCode)
	if errors.Is(err, sql.ErrNoRows) {
		return j, httpx.NotFound()
	}
	if err != nil {
		return j, err
	}
	if _, _, err := s.load(ctx, s.DB, subject, j.SourceID, false); err != nil {
		return Job{}, err
	}
	if j.Status == "failed" {
		j.Error = &JobError{Code: errCode.String, Message: jobErrorText(errCode.String), RequestID: j.ID}
	}
	return j, nil
}

func jobErrorText(code string) string {
	if code == codeIndexDisabled {
		return "Поиск по материалам на этом стенде не подключён: материал сохранён, но в ответах не используется."
	}
	return "Не удалось обновить материал в поиске. Для ответов остаётся прежняя версия."
}

const (
	codeIndexDisabled = "INDEX_DISABLED"
	codeIndexFailed   = "INDEX_FAILED"
	maxSubmitAttempts = 5
)

// Run is the index worker: on Trigger and every interval.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		for {
			ok, err := s.Step(ctx)
			if err != nil && ctx.Err() == nil && s.Log != nil {
				s.Log.Error("sources worker", "err", err.Error())
			}
			if !ok || err != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.wakeup():
		}
	}
}

type pending struct {
	id, source, version, action, status string
	external                            sql.NullString
	attempts                            int
}

// Step advances one due index job: submit it to the knowledge service, then follow its status.
func (s *Service) Step(ctx context.Context) (bool, error) {
	now := s.now()
	var p pending
	err := s.DB.QueryRowContext(ctx, `UPDATE source_jobs SET available_at=$2, updated_at=$1
		WHERE id = (SELECT id FROM source_jobs WHERE status IN ('queued','running') AND available_at <= $1
			ORDER BY created_at, id FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING id, source_id, candidate_version_id, action, status, external_job_id, attempts`, now, now.Add(s.poll())).
		Scan(&p.id, &p.source, &p.version, &p.action, &p.status, &p.external, &p.attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if s.Index == nil {
		return true, s.finish(ctx, p, "failed", codeIndexDisabled)
	}
	if p.status == "queued" {
		return true, s.submit(ctx, p)
	}
	status, attempts, err := s.Index.Job(ctx, p.external.String)
	switch {
	case err != nil:
		return true, nil // checked again after the poll interval
	case status == "completed":
		return true, s.finish(ctx, p, "ready", "")
	case status == "failed" && attempts >= 3: // the service retried and gave up
		return true, s.finish(ctx, p, "failed", codeIndexFailed)
	}
	return true, nil // pending or processing: wait
}

func (s *Service) submit(ctx context.Context, p pending) error {
	var ext string
	var err error
	if p.action == "remove" {
		ext, err = s.Index.Remove(ctx, p.id, p.source)
	} else {
		var doc Document
		if doc, err = s.document(ctx, p); err == nil {
			ext, err = s.Index.Put(ctx, p.id, doc) // the job id is the idempotency key: a retry is the same request
		}
	}
	if err != nil {
		if p.attempts+1 >= maxSubmitAttempts {
			return s.finish(ctx, p, "failed", codeIndexFailed)
		}
		_, err = s.DB.ExecContext(ctx, `UPDATE source_jobs SET attempts=attempts+1, available_at=$2 WHERE id=$1 AND status='queued'`,
			p.id, s.now().Add(s.poll()<<(p.attempts+1)))
		if err == nil {
			s.Metrics.Job(metrics.KindIndex, metrics.OutcomeRetry)
		}
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE source_jobs SET status='running', external_job_id=$2, attempts=attempts+1 WHERE id=$1 AND status='queued'`, p.id, ext)
	return err
}

func (s *Service) document(ctx context.Context, p pending) (Document, error) {
	var d Document
	var acl []byte
	var created time.Time
	err := s.DB.QueryRowContext(ctx, `SELECT title, body, acl, created_at FROM source_versions WHERE id=$1`, p.version).Scan(&d.Title, &d.Text, &acl, &created)
	if err != nil {
		return d, err
	}
	var a ACL
	if err := json.Unmarshal(acl, &a); err != nil {
		return d, err
	}
	team := a.TeamIDs[0]
	d.DocumentID, d.Revision, d.URL, d.Scope, d.ScopeID = p.source, p.version, strings.TrimRight(s.URLBase, "/")+"/"+p.source, "team", &team
	d.ValidFrom, d.ExpiresAt = created.UTC(), created.UTC().AddDate(10, 0, 0)
	return d, nil
}

// finish: the final status of the job; an indexed version becomes the one answers use.
func (s *Service) finish(ctx context.Context, p pending, status, code string) error {
	finished := false
	err := db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE source_jobs SET status=$2, error_code=NULLIF($3,''), updated_at=$4 WHERE id=$1 AND status IN ('queued','running')`,
			p.id, status, code, s.now())
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		finished = n > 0
		if n == 0 || status != "ready" {
			return nil
		}
		if p.action == "remove" {
			_, err = tx.ExecContext(ctx, `UPDATE sources SET active_version_id=NULL WHERE id=$1`, p.source)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE sources SET active_version_id=$2 WHERE id=$1`, p.source, p.version)
		}
		return err
	})
	if err == nil && finished { // a repeated finish of an already closed job is not counted
		outcome := metrics.OutcomeFailed
		if status == "ready" {
			outcome = metrics.OutcomeCompleted
		}
		s.Metrics.Job(metrics.KindIndex, outcome)
	}
	return err
}
