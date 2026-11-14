package answers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/ilhenyco/ai-buddy/backend/internal/identity"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/idempotency"
)

// Routes: askQuestion and getAnswer of the contract.
func (s *Service) Routes(mux *http.ServeMux, prefix string, auth func(http.Handler) http.Handler) {
	mux.Handle("POST "+prefix+"/assignments/{assignmentId}/questions", auth(httpx.Handler(s.handleAsk)))
	mux.Handle("GET "+prefix+"/answers/{answerId}", auth(httpx.Handler(s.handleGet)))
}

func subject(r *http.Request) string {
	p, _ := identity.Subject(r.Context())
	return p.ID
}

func (s *Service) handleAsk(w http.ResponseWriter, r *http.Request) error {
	key, err := idempotency.Key(r)
	if err != nil {
		return err
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	status, body, err := s.Ask(r.Context(), subject(r), r.PathValue("assignmentId"), key, in.Text)
	var limited *RateLimited
	if errors.As(err, &limited) {
		w.Header().Set("Retry-After", strconv.Itoa(int(limited.RetryAfter.Seconds())))
		return &httpx.Error{Status: http.StatusTooManyRequests, Code: "RATE_LIMITED",
			Message: "Слишком много вопросов подряд. Подожди немного и спроси снова."}
	}
	if err != nil {
		return err
	}
	httpx.WriteRawJSON(w, status, body)
	return nil
}

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) error {
	j, err := s.Get(r.Context(), subject(r), r.PathValue("answerId"))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, j)
	return nil
}
