// Package identity: who is calling and which assignments they belong to.
// Owner: Бэк-2. The scenarios module only uses Subject, Assignments and LeadAssignment.
//
// SSO is not agreed yet (server-handoff.md), so the session is a signed cookie created
// by the demo login. The role in the browser is never trusted: relations come from the DB.
package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
)

const (
	CookieName = "homych_session"
	SessionTTL = 8 * time.Hour

	CapScenariosWrite = "scenarios:write"
	CapFeedRead       = "feed:read"
	CapQuestionsAsk   = "questions:ask"
)

type Person struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type Assignment struct {
	ID           string   `json:"id"`
	Intern       Person   `json:"intern"`
	Buddy        Person   `json:"buddy"`
	Lead         Person   `json:"lead"`
	TeamID       string   `json:"teamId"`
	Capabilities []string `json:"capabilities"`
}

type Context struct {
	Subject      Person       `json:"subject"`
	Assignments  []Assignment `json:"assignments"`
	Capabilities []string     `json:"capabilities"`
	ServerTime   time.Time    `json:"serverTime"`
	CSRFToken    string       `json:"csrfToken"`
}

// Service verifies sessions and reads the directory.
type Service struct {
	DB             *sql.DB
	Secret         []byte
	AllowedOrigins []string
	Now            func() time.Time
	SecureCookie   bool
}

type ctxKey int

const (
	subjectKey ctxKey = iota
	cookieKey
)

// Subject returns the authenticated user (set by Authenticate).
func Subject(ctx context.Context) (Person, bool) {
	p, ok := ctx.Value(subjectKey).(Person)
	return p, ok
}

// WithSubject is for tests of other modules.
func WithSubject(ctx context.Context, p Person) context.Context {
	return context.WithValue(ctx, subjectKey, p)
}

func (s *Service) sign(payload string) string {
	m := hmac.New(sha256.New, s.Secret)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// NewSessionValue returns a cookie value "userId|expiresUnix.signature".
func (s *Service) NewSessionValue(userID string) string {
	payload := userID + "|" + strconv.FormatInt(s.Now().Add(SessionTTL).Unix(), 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + s.sign(payload)
}

func (s *Service) parseSession(v string) (string, bool) {
	enc, sig, ok := strings.Cut(v, ".")
	if !ok {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return "", false
	}
	payload := string(raw)
	if !hmac.Equal([]byte(sig), []byte(s.sign(payload))) {
		return "", false
	}
	userID, expS, ok := strings.Cut(payload, "|")
	exp, err := strconv.ParseInt(expS, 10, 64)
	if !ok || err != nil || s.Now().Unix() > exp {
		return "", false
	}
	return userID, true
}

// CSRFToken is bound to the session cookie value.
func (s *Service) CSRFToken(cookieValue string) string {
	m := hmac.New(sha256.New, s.Secret)
	m.Write([]byte("csrf|" + cookieValue))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

// Authenticate requires a valid session; for unsafe methods it also checks Origin and X-CSRF-Token.
func (s *Service) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil {
			httpx.WriteError(w, r, httpx.Unauthenticated())
			return
		}
		userID, ok := s.parseSession(c.Value)
		if !ok {
			httpx.WriteError(w, r, httpx.Unauthenticated())
			return
		}
		p, err := s.Person(r.Context(), userID)
		if err != nil {
			httpx.WriteError(w, r, httpx.Unauthenticated())
			return
		}
		if unsafeMethod(r.Method) {
			if err := s.checkOrigin(r); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			if !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRFToken(c.Value))) {
				httpx.WriteError(w, r, httpx.Forbidden("Неверный CSRF-токен. Обновите страницу."))
				return
			}
		}
		ctx := context.WithValue(r.Context(), subjectKey, p)
		ctx = context.WithValue(ctx, cookieKey, c.Value)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func unsafeMethod(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions
}

func (s *Service) checkOrigin(r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil // non-browser clients; the CSRF token is still required
	}
	if origin == "http://"+r.Host || origin == "https://"+r.Host {
		return nil
	}
	for _, o := range s.AllowedOrigins {
		if origin == o {
			return nil
		}
	}
	return httpx.Forbidden("Запрос с недопустимого адреса.")
}

func (s *Service) Person(ctx context.Context, id string) (Person, error) {
	p := Person{ID: id}
	err := s.DB.QueryRowContext(ctx, `SELECT display_name FROM users WHERE id=$1`, id).Scan(&p.DisplayName)
	if errors.Is(err, sql.ErrNoRows) {
		return p, httpx.NotFound()
	}
	return p, err
}

const assignmentSelect = `SELECT a.id, a.team_id,
	i.id, i.display_name, b.id, b.display_name, l.id, l.display_name
	FROM assignments a
	JOIN users i ON i.id=a.intern_id JOIN users b ON b.id=a.buddy_id JOIN users l ON l.id=a.lead_id `

// Querier is satisfied by *sql.DB and *sql.Tx.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func scanAssignments(rows *sql.Rows, subjectID string) ([]Assignment, error) {
	defer rows.Close()
	out := []Assignment{}
	for rows.Next() {
		var a Assignment
		if err := rows.Scan(&a.ID, &a.TeamID, &a.Intern.ID, &a.Intern.DisplayName,
			&a.Buddy.ID, &a.Buddy.DisplayName, &a.Lead.ID, &a.Lead.DisplayName); err != nil {
			return nil, err
		}
		a.Capabilities = capabilities(a, subjectID)
		out = append(out, a)
	}
	return out, rows.Err()
}

func capabilities(a Assignment, subjectID string) []string {
	switch subjectID {
	case a.Intern.ID:
		return []string{CapFeedRead, CapQuestionsAsk}
	case a.Lead.ID:
		return []string{CapFeedRead, CapScenariosWrite}
	case a.Buddy.ID:
		return []string{CapFeedRead}
	}
	return []string{}
}

// Assignments returns every assignment where the subject is intern, buddy or lead.
func Assignments(ctx context.Context, q Querier, subjectID string) ([]Assignment, error) {
	rows, err := q.QueryContext(ctx, assignmentSelect+
		`WHERE $1 IN (a.intern_id, a.buddy_id, a.lead_id) ORDER BY i.display_name, a.id`, subjectID)
	if err != nil {
		return nil, err
	}
	return scanAssignments(rows, subjectID)
}

// LeadAssignment returns the assignment only if the subject is its lead; otherwise NOT_FOUND.
func LeadAssignment(ctx context.Context, q Querier, leadID, assignmentID string) (Assignment, error) {
	rows, err := q.QueryContext(ctx, assignmentSelect+`WHERE a.id=$1 AND a.lead_id=$2`, assignmentID, leadID)
	if err != nil {
		return Assignment{}, err
	}
	list, err := scanAssignments(rows, leadID)
	if err != nil {
		return Assignment{}, err
	}
	if len(list) == 0 {
		return Assignment{}, httpx.NotFound()
	}
	return list[0], nil
}

// LeadAssignmentByIntern finds the lead's assignment for an intern user id.
func LeadAssignmentByIntern(ctx context.Context, q Querier, leadID, internID string) (Assignment, error) {
	rows, err := q.QueryContext(ctx, assignmentSelect+`WHERE a.intern_id=$1 AND a.lead_id=$2 ORDER BY a.id LIMIT 1`, internID, leadID)
	if err != nil {
		return Assignment{}, err
	}
	list, err := scanAssignments(rows, leadID)
	if err != nil {
		return Assignment{}, err
	}
	if len(list) == 0 {
		return Assignment{}, httpx.NotFound()
	}
	return list[0], nil
}

// IsLead reports whether the subject leads at least one assignment.
func IsLead(ctx context.Context, q Querier, subjectID string) (bool, error) {
	var ok bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM assignments WHERE lead_id=$1)`, subjectID).Scan(&ok)
	return ok, err
}

// BuildContext is the body of GET /session/context.
func (s *Service) BuildContext(ctx context.Context, p Person, cookieValue string) (Context, error) {
	list, err := Assignments(ctx, s.DB, p.ID)
	if err != nil {
		return Context{}, err
	}
	seen := map[string]bool{}
	caps := []string{}
	for _, a := range list {
		for _, c := range a.Capabilities {
			if !seen[c] {
				seen[c] = true
				caps = append(caps, c)
			}
		}
	}
	return Context{Subject: p, Assignments: list, Capabilities: caps,
		ServerTime: s.Now().UTC(), CSRFToken: s.CSRFToken(cookieValue)}, nil
}
