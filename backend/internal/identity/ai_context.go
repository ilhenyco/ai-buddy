package identity

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
)

// AIContext is the only identity data exported to the Python knowledge service.
// The service resolves current assignments on every call; source text cannot grant rights.
type AIContext struct {
	SubjectID     string   `json:"subjectId"`
	TeamIDs       []string `json:"teamIds"`
	InternshipIDs []string `json:"internshipIds"`
	Role          string   `json:"role"`
	Synthetic     bool     `json:"synthetic"`
}

// AIRoutes is disabled unless a dedicated service credential is configured.
// It does not accept browser cookies or roles/team IDs supplied by callers.
func (s *Service) AIRoutes(mux *http.ServeMux, token string) {
	if len(token) < 24 {
		return
	}
	mux.Handle("POST /internal/v1/ai-context", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		provided, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			return httpx.Unauthenticated()
		}
		var input struct {
			SubjectID string `json:"subjectId"`
		}
		if err := httpx.DecodeJSON(r, &input); err != nil {
			return err
		}
		if input.SubjectID == "" || len(input.SubjectID) > 80 {
			return httpx.Validation("Нужен subjectId.", "subjectId")
		}
		if _, err := s.Person(r.Context(), input.SubjectID); err != nil {
			return err
		}
		assignments, err := Assignments(r.Context(), s.DB, input.SubjectID)
		if err != nil {
			return err
		}
		out := AIContext{SubjectID: input.SubjectID, TeamIDs: []string{}, InternshipIDs: []string{}, Role: "intern"}
		seen := map[string]bool{}
		for _, a := range assignments {
			if a.Intern.ID != input.SubjectID {
				continue
			}
			out.InternshipIDs = append(out.InternshipIDs, a.ID)
			if !seen[a.TeamID] {
				out.TeamIDs = append(out.TeamIDs, a.TeamID)
				seen[a.TeamID] = true
			}
		}
		if len(out.InternshipIDs) == 0 {
			return httpx.Forbidden("Личный поиск знаний доступен назначенному стажёру.")
		}
		httpx.WriteJSON(w, http.StatusOK, out)
		return nil
	}))
}
