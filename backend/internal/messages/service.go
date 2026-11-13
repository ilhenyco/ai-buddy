package messages

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
)

// member returns the subject's role in the assignment, or NOT_FOUND when the subject
// is not a participant (the feed of other people is invisible).
func (s *Service) member(ctx context.Context, subject, assignmentID string) (string, error) {
	var intern, buddy, lead string
	err := s.DB.QueryRowContext(ctx, `SELECT intern_id, buddy_id, lead_id FROM assignments WHERE id=$1`, assignmentID).
		Scan(&intern, &buddy, &lead)
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

func page(cursor string, limit, def, max int) (int, int, error) {
	if limit == 0 {
		limit = def
	}
	if limit < 1 || limit > max {
		return 0, 0, httpx.Validation("limit должен быть от 1 до "+strconv.Itoa(max)+".", "limit")
	}
	offset := 0
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 0 {
			return 0, 0, httpx.Validation("Некорректный cursor.", "cursor")
		}
		offset = n
	}
	return offset, limit, nil
}

func nextCursor(offset, limit, got int) *string {
	if got <= limit {
		return nil
	}
	c := strconv.Itoa(offset + limit)
	return &c
}

func scanMessage(rows *sql.Rows) (Message, error) {
	var m Message
	var block []byte
	var next sql.NullString
	if err := rows.Scan(&m.ID, &m.AssignmentID, &m.Kind, &m.RecipientUserID, &m.CreatedAt, &m.Text, &block, &next, &m.ScenarioID, &m.RevisionID); err != nil {
		return m, err
	}
	m.CreatedAt = m.CreatedAt.UTC()
	m.Block = &scenarios.Block{}
	if err := json.Unmarshal(block, m.Block); err != nil {
		return m, err
	}
	if next.Valid {
		m.Next = &NextBlock{}
		if err := json.Unmarshal([]byte(next.String), m.Next); err != nil {
			return m, err
		}
	}
	return m, nil
}

const messageColumns = `id, assignment_id, kind, recipient_user_id, created_at, text, block, next_block, scenario_id, revision_id`

// Feed — GET /assignments/{id}/messages: the subject's own messages in time order, with own checks.
func (s *Service) Feed(ctx context.Context, subject, assignmentID, cursor string, limit int) (FeedPage, error) {
	if _, err := s.member(ctx, subject, assignmentID); err != nil {
		return FeedPage{}, err
	}
	offset, limit, err := page(cursor, limit, 100, 200)
	if err != nil {
		return FeedPage{}, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+messageColumns+` FROM messages
		WHERE assignment_id=$1 AND recipient_user_id=$2 ORDER BY created_at, id OFFSET $3 LIMIT $4`,
		assignmentID, subject, offset, limit+1)
	if err != nil {
		return FeedPage{}, err
	}
	defer rows.Close()
	out := FeedPage{Items: []Message{}, Checks: []Check{}, ServerTime: s.now()}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return FeedPage{}, err
		}
		out.Items = append(out.Items, m)
	}
	if err := rows.Err(); err != nil {
		return FeedPage{}, err
	}
	out.NextCursor = nextCursor(offset, limit, len(out.Items))
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
	}

	checks, err := s.DB.QueryContext(ctx, `SELECT c.message_id, c.task_id, c.user_id, c.checked, c.updated_at
		FROM message_checks c JOIN messages m ON m.id=c.message_id
		WHERE m.assignment_id=$1 AND c.user_id=$2 ORDER BY c.message_id, c.task_id`, assignmentID, subject)
	if err != nil {
		return FeedPage{}, err
	}
	defer checks.Close()
	for checks.Next() {
		var c Check
		if err := checks.Scan(&c.MessageID, &c.TaskID, &c.UserID, &c.Checked, &c.UpdatedAt); err != nil {
			return FeedPage{}, err
		}
		c.UpdatedAt = c.UpdatedAt.UTC()
		out.Checks = append(out.Checks, c)
	}
	if err := checks.Err(); err != nil {
		return FeedPage{}, err
	}

	var info ActivePlanInfo
	var plan []byte
	err = s.DB.QueryRowContext(ctx, `SELECT a.scenario_id, a.revision_id, r.plan FROM active_scenarios a
		JOIN scenario_revisions r ON r.id=a.revision_id WHERE a.assignment_id=$1`, assignmentID).Scan(&info.ScenarioID, &info.RevisionID, &plan)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return FeedPage{}, err
	default:
		var p scenarios.Plan
		if err := json.Unmarshal(plan, &p); err != nil {
			return FeedPage{}, err
		}
		info.Start, info.Months = p.Start, p.Months
		out.ActivePlan = &info
	}
	return out, nil
}

// SetCheck — PUT /messages/{id}/tasks/{taskId}/check. Only the recipient marks own visible items.
// The mark is stored and never read by the scheduler: the next block comes by date anyway.
func (s *Service) SetCheck(ctx context.Context, subject, messageID, taskID string, checked bool) (Check, error) {
	var role string
	var block []byte
	err := s.DB.QueryRowContext(ctx, `SELECT recipient_role, block FROM messages WHERE id=$1 AND recipient_user_id=$2`, messageID, subject).
		Scan(&role, &block)
	if errors.Is(err, sql.ErrNoRows) {
		return Check{}, httpx.NotFound()
	}
	if err != nil {
		return Check{}, err
	}
	var b scenarios.Block
	if err := json.Unmarshal(block, &b); err != nil {
		return Check{}, err
	}
	if !taskVisible(b, role, taskID) {
		return Check{}, httpx.NotFound()
	}
	c := Check{MessageID: messageID, TaskID: taskID, UserID: subject, Checked: checked, UpdatedAt: s.now()}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO message_checks(message_id, task_id, user_id, checked, updated_at)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (message_id, task_id, user_id) DO UPDATE SET checked=EXCLUDED.checked, updated_at=EXCLUDED.updated_at`,
		c.MessageID, c.TaskID, c.UserID, c.Checked, c.UpdatedAt)
	return c, err
}

// taskVisible: the intern sees every task; buddy and lead only tasks with an action for them.
func taskVisible(b scenarios.Block, role, taskID string) bool {
	for _, t := range b.Tasks {
		if t.ID != taskID {
			continue
		}
		switch role {
		case "intern":
			return true
		case "buddy":
			return t.Actions != nil && t.Actions.Buddy != ""
		case "lead":
			return t.Actions != nil && t.Actions.Lead != ""
		}
	}
	return false
}

// Notifications — GET /notifications: own messages, newest first. In the web channel a stored
// message is delivered; push or messenger delivery is an open question (OQ02).
func (s *Service) Notifications(ctx context.Context, subject, cursor string, limit int) (NotificationPage, error) {
	offset, limit, err := page(cursor, limit, 30, 100)
	if err != nil {
		return NotificationPage{}, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT m.id, m.assignment_id, m.created_at, m.block->>'title', m.recipient_role, u.display_name
		FROM messages m JOIN assignments a ON a.id=m.assignment_id JOIN users u ON u.id=a.intern_id
		WHERE m.recipient_user_id=$1 ORDER BY m.created_at DESC, m.id DESC OFFSET $2 LIMIT $3`, subject, offset, limit+1)
	if err != nil {
		return NotificationPage{}, err
	}
	defer rows.Close()
	out := NotificationPage{Items: []Notification{}}
	for rows.Next() {
		var n Notification
		var title, role, intern string
		if err := rows.Scan(&n.MessageID, &n.AssignmentID, &n.CreatedAt, &title, &role, &intern); err != nil {
			return NotificationPage{}, err
		}
		n.ID, n.CreatedAt, n.DeliveryStatus = "ntf-"+n.MessageID, n.CreatedAt.UTC(), "sent"
		n.Title = "Хомыч: " + title
		if role != "intern" {
			n.Title += " · " + intern
		}
		out.Items = append(out.Items, n)
	}
	if err := rows.Err(); err != nil {
		return NotificationPage{}, err
	}
	out.NextCursor = nextCursor(offset, limit, len(out.Items))
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
	}
	return out, nil
}

// today is the scheduler's calendar date (moves with the demo clock).
func (s *Service) today() string { return s.Clock.Now().In(s.Loc).Format(time.DateOnly) }
