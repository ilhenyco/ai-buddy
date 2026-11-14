// Package answers: the intern's personal questions to Хомыч (askQuestion, getAnswer).
//
// The question and its AnswerJob are stored BEFORE the knowledge service is called. A worker
// claims a job with a lease, calls the Python RAG service and stores the answer exactly once
// (fencing by the attempt number). A failure is retried with backoff; after the last attempt
// the job fails with an honest message in the chat, and the question is never lost.
// The calendar does not depend on AI: messages and scenarios work with the service down.
package answers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ilhenyco/ai-buddy/backend/internal/messages"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/db"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/idempotency"
)

const (
	MaxQuestion = 1000 // runes, as the knowledge service accepts
	MaxContext  = 4000 // runes of messageContext
)

// Job is the contract's AnswerJob.
type Job struct {
	ID                string            `json:"id"`
	AssignmentID      string            `json:"assignmentId"`
	QuestionMessageID string            `json:"questionMessageId"`
	Status            string            `json:"status"`
	Answer            *messages.Message `json:"answer"`
	Error             *JobError         `json:"error"`
	CreatedAt         time.Time         `json:"createdAt"`
}

type JobError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}

type Service struct {
	DB *sql.DB
	// Clock is real time (leases, backoff, rate limit); Calendar dates chat messages like the
	// checkpoints, so that the feed stays in order when the demo date is moved.
	Clock     clock.Clock
	Calendar  clock.Clock
	Knowledge Knowledge // nil: AI is switched off on this stand, questions get an honest reply
	Log       *slog.Logger

	MaxAttempts int           // default 3
	Backoff     time.Duration // default 5s, doubled after each failed attempt
	Lease       time.Duration // must exceed the knowledge client timeout; default 5m
	RatePerMin  int           // questions per author per minute; default 6

	once sync.Once
	wake chan struct{}
}

func (s *Service) defaults() {
	if s.MaxAttempts == 0 {
		s.MaxAttempts = 3
	}
	if s.Backoff == 0 {
		s.Backoff = 5 * time.Second
	}
	if s.Lease == 0 {
		s.Lease = 5 * time.Minute
	}
	if s.RatePerMin == 0 {
		s.RatePerMin = 6
	}
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func (s *Service) now() time.Time      { return s.Clock.Now().UTC().Truncate(time.Microsecond) }
func (s *Service) calendar() time.Time { return s.Calendar.Now().UTC().Truncate(time.Microsecond) }

func (s *Service) wakeup() chan struct{} {
	s.once.Do(func() { s.wake = make(chan struct{}, 1) })
	return s.wake
}

// Trigger asks the worker to look for jobs now. Never blocks.
func (s *Service) Trigger() {
	select {
	case s.wakeup() <- struct{}{}:
	default:
	}
}

// RateLimited carries Retry-After for 429.
type RateLimited struct{ RetryAfter time.Duration }

func (e *RateLimited) Error() string { return "too many questions" }

// Ask — POST /assignments/{id}/questions: only the intern of the assignment, 202 with the job.
func (s *Service) Ask(ctx context.Context, subject, assignmentID, key, text string) (int, []byte, error) {
	s.defaults()
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > MaxQuestion {
		return 0, nil, httpx.Validation("Напишите вопрос (до 1000 символов).", "text")
	}
	var intern, buddy, lead string
	err := s.DB.QueryRowContext(ctx, `SELECT intern_id, buddy_id, lead_id FROM assignments WHERE id=$1`, assignmentID).Scan(&intern, &buddy, &lead)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, httpx.NotFound()
	}
	if err != nil {
		return 0, nil, err
	}
	switch subject {
	case intern:
	case buddy, lead:
		return 0, nil, httpx.Forbidden("Личный чат с Хомычем есть только у стажёра.")
	default:
		return 0, nil, httpx.NotFound()
	}
	hash, err := idempotency.Hash(struct{ A, T string }{assignmentID, text})
	if err != nil {
		return 0, nil, err
	}
	var status int
	var body []byte
	err = db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		rp, err := idempotency.Begin(ctx, tx, subject, "askQuestion", key, hash)
		if err != nil || rp != nil {
			if rp != nil {
				status, body = rp.Status, rp.Body
			}
			return err
		}
		now := s.now()
		var recent int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM answer_jobs WHERE author_id=$1 AND created_at > $2`,
			subject, now.Add(-time.Minute)).Scan(&recent); err != nil {
			return err
		}
		if recent >= s.RatePerMin {
			return &RateLimited{RetryAfter: 30 * time.Second}
		}
		job := Job{ID: newID("ans-"), AssignmentID: assignmentID, QuestionMessageID: newID("msg-"), Status: "queued", CreatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages(id, assignment_id, recipient_user_id, recipient_role, kind, text, answer_request_id, created_at)
			VALUES ($1,$2,$3,'intern','user',$4,$5,$6)`, job.QuestionMessageID, assignmentID, subject, text, job.ID, s.calendar()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO answer_jobs(id, assignment_id, author_id, question_message_id, question, status, available_at, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,'queued',$6,$6,$6)`, job.ID, assignmentID, subject, job.QuestionMessageID, text, now); err != nil {
			return err
		}
		status = http.StatusAccepted
		body, err = idempotency.Save(ctx, tx, subject, "askQuestion", key, hash, status, job)
		return err
	})
	if err != nil {
		return 0, nil, err
	}
	s.Trigger()
	return status, body, nil
}

// Get — GET /answers/{id}: only the author of the question.
func (s *Service) Get(ctx context.Context, subject, id string) (Job, error) {
	var j Job
	var answerID, errCode sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT id, assignment_id, question_message_id, status, answer_message_id, error_code, created_at
		FROM answer_jobs WHERE id=$1 AND author_id=$2`, id, subject).
		Scan(&j.ID, &j.AssignmentID, &j.QuestionMessageID, &j.Status, &answerID, &errCode, &j.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return j, httpx.NotFound()
	}
	if err != nil {
		return j, err
	}
	j.CreatedAt = j.CreatedAt.UTC()
	if answerID.Valid {
		m, err := s.message(ctx, answerID.String)
		if err != nil {
			return j, err
		}
		j.Answer = &m
	}
	if j.Status == "failed" {
		j.Error = &JobError{Code: errCode.String, Message: failText(errCode.String), RequestID: j.ID}
	}
	return j, nil
}

func (s *Service) message(ctx context.Context, id string) (messages.Message, error) {
	var m messages.Message
	var citations []byte
	var errCode, request sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT id, assignment_id, kind, recipient_user_id, created_at, text, citations, answer_request_id, error_code
		FROM messages WHERE id=$1`, id).Scan(&m.ID, &m.AssignmentID, &m.Kind, &m.RecipientUserID, &m.CreatedAt, &m.Text, &citations, &request, &errCode)
	if err != nil {
		return m, err
	}
	m.CreatedAt, m.AnswerRequestID, m.ErrorCode = m.CreatedAt.UTC(), request.String, errCode.String
	if citations != nil {
		err = json.Unmarshal(citations, &m.Citations)
	}
	return m, err
}

const (
	codeUnavailable = "AI_UNAVAILABLE"
	codeRejected    = "AI_REJECTED"
	codeDisabled    = "AI_DISABLED"
)

func failText(code string) string {
	switch code {
	case codeDisabled:
		return "Поиск по материалам на этом стенде не подключён. Вопрос сохранён — задай его бадди."
	case codeRejected:
		return "Не получилось найти ответ в доступных тебе материалах. Вопрос сохранён — задай его бадди."
	}
	return "Сейчас не получилось ответить: база знаний недоступна. Вопрос сохранён — спроси ещё раз позже или напиши бадди."
}

// Run is the worker loop: on Trigger and every interval, until ctx is done.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		for {
			ok, err := s.Step(ctx)
			if err != nil && ctx.Err() == nil && s.Log != nil {
				s.Log.Error("answers worker", "err", err.Error())
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

type claimed struct {
	id, assignment, author, question string
	attempt                          int
}

// Step processes one due job; false when there was none.
func (s *Service) Step(ctx context.Context) (bool, error) {
	s.defaults()
	now := s.now()
	var c claimed
	err := s.DB.QueryRowContext(ctx, `UPDATE answer_jobs SET status='running', attempts=attempts+1, lease_until=$2, updated_at=$1
		WHERE id = (SELECT id FROM answer_jobs
			WHERE (status='queued' AND available_at <= $1) OR (status='running' AND lease_until < $1)
			ORDER BY available_at, id FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING id, assignment_id, author_id, question, attempts`, now, now.Add(s.Lease)).
		Scan(&c.id, &c.assignment, &c.author, &c.question, &c.attempt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, s.process(ctx, c)
}

func (s *Service) process(ctx context.Context, c claimed) error {
	if s.Knowledge == nil {
		return s.fail(ctx, c, codeDisabled)
	}
	in := RAGInput{SubjectID: c.author, RequestID: c.id, Query: c.question}
	if mc, err := s.stageContext(ctx, c); err != nil {
		return err
	} else if mc != "" {
		in.MessageContext = &mc
	}
	res, err := s.Knowledge.Answer(ctx, in)
	var rejected *RejectedError
	switch {
	case errors.As(err, &rejected):
		return s.fail(ctx, c, codeRejected)
	case err != nil: // the service is down or timed out: try again later
		if c.attempt < s.MaxAttempts {
			return s.retry(ctx, c)
		}
		return s.fail(ctx, c, codeUnavailable)
	case res.Status == "unavailable":
		// the service answered but could not prepare an answer (model or embedding failed inside):
		// an expected outcome of the contract, not retried — a slow model would keep the chat waiting
		return s.fail(ctx, c, codeUnavailable)
	}
	return s.complete(ctx, c, res)
}

// stageContext: the intern's latest checkpoint (title and own text), so that the answer can be
// tied to the current stage. Only this intern's own message, never buddy's or lead's.
func (s *Service) stageContext(ctx context.Context, c claimed) (string, error) {
	var title, text string
	err := s.DB.QueryRowContext(ctx, `SELECT block->>'title', text FROM messages
		WHERE assignment_id=$1 AND recipient_user_id=$2 AND recipient_role='intern' AND kind='checkpoint'
		ORDER BY created_at DESC, id DESC LIMIT 1`, c.assignment, c.author).Scan(&title, &text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	mc := "Текущий этап стажёра: " + title + ". " + text
	if r := []rune(mc); len(r) > MaxContext {
		mc = string(r[:MaxContext])
	}
	return mc, nil
}

func (s *Service) retry(ctx context.Context, c claimed) error {
	delay := s.Backoff << (c.attempt - 1)
	_, err := s.DB.ExecContext(ctx, `UPDATE answer_jobs SET status='queued', lease_until=NULL, available_at=$3, updated_at=$2
		WHERE id=$1 AND status='running' AND attempts=$4`, c.id, s.now(), s.now().Add(delay), c.attempt)
	return err
}

// finish stores the answer message and the final status in one transaction, only if this
// worker still holds the job (same attempt): a late duplicate does nothing.
func (s *Service) finish(ctx context.Context, c claimed, status, text, errCode string, citations []messages.Citation, res *RAGResult) error {
	return db.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		var held bool
		if err := tx.QueryRowContext(ctx, `SELECT status='running' AND attempts=$2 FROM answer_jobs WHERE id=$1 FOR UPDATE`, c.id, c.attempt).Scan(&held); err != nil || !held {
			return err
		}
		msgID := newID("msg-")
		var cj any // NULL without sources
		if len(citations) > 0 {
			b, _ := json.Marshal(citations)
			cj = string(b)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages(id, assignment_id, recipient_user_id, recipient_role, kind, text, citations, answer_request_id, error_code, created_at)
			VALUES ($1,$2,$3,'intern','assistant',$4,$5,$6,NULLIF($7,''),
				-- always after its question in the feed, even within the same clock tick
				GREATEST($8, (SELECT q.created_at + interval '1 millisecond' FROM answer_jobs j JOIN messages q ON q.id=j.question_message_id WHERE j.id=$6)))`,
			msgID, c.assignment, c.author, text, cj, c.id, errCode, s.calendar()); err != nil {
			return err
		}
		var outcome, mode, snapshot *string
		if res != nil {
			outcome, mode, snapshot = &res.Status, &res.RetrievalMode, res.SnapshotID
		}
		_, err := tx.ExecContext(ctx, `UPDATE answer_jobs SET status=$2, answer_message_id=$3, error_code=NULLIF($4,''), outcome=$5,
			retrieval_mode=$6, snapshot_id=$7, lease_until=NULL, updated_at=$8 WHERE id=$1`, c.id, status, msgID, errCode, outcome, mode, snapshot, s.now())
		return err
	})
}

func (s *Service) fail(ctx context.Context, c claimed, code string) error {
	return s.finish(ctx, c, "failed", failText(code), code, nil, nil)
}

func (s *Service) complete(ctx context.Context, c claimed, res RAGResult) error {
	text := strings.TrimSpace(res.Reply)
	switch {
	case res.Status == "no_evidence" && text == "":
		text = "В доступных материалах нет ответа на этот вопрос. Спроси бадди — он подскажет."
	case res.Status == "conflicting_sources":
		text += "\n\nИсточники расходятся — уточни у бадди, как принято в команде сейчас."
	case text == "":
		text = "Не получилось сформулировать ответ. Спроси бадди."
	}
	citations := make([]messages.Citation, 0, len(res.Citations))
	seen := map[string]bool{}
	for _, rc := range res.Citations {
		k := rc.DocumentID + "\x1f" + rc.Section
		if seen[k] {
			continue
		}
		seen[k] = true
		citations = append(citations, messages.Citation{SourceID: rc.DocumentID, SourceVersionID: rc.Revision, Title: rc.Title, Fragment: rc.Section, URL: rc.URL})
	}
	return s.finish(ctx, c, "completed", text, "", citations, &res)
}
