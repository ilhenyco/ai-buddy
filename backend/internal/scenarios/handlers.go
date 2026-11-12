package scenarios

import (
	"net/http"
	"strconv"

	"github.com/ilhenyco/ai-buddy/backend/internal/identity"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/idempotency"
)

// Routes registers the six scenario operations. auth wraps every handler with the session check.
func (s *Service) Routes(mux *http.ServeMux, prefix string, auth func(http.Handler) http.Handler) {
	h := func(pattern string, fn httpx.Handler) { mux.Handle(pattern, auth(fn)) }
	h("GET "+prefix+"/scenario-templates", s.handleTemplates)
	h("GET "+prefix+"/scenarios", s.handleList)
	h("POST "+prefix+"/scenarios", s.handleCreate)
	h("GET "+prefix+"/scenarios/{scenarioId}", s.handleGet)
	h("PUT "+prefix+"/scenarios/{scenarioId}/draft", s.handleSaveDraft)
	h("POST "+prefix+"/scenarios/{scenarioId}/publications", s.handlePublish)
}

func subject(r *http.Request) string {
	p, _ := identity.Subject(r.Context())
	return p.ID
}

func write(w http.ResponseWriter, res Result) {
	if res.ETag != "" {
		w.Header().Set("ETag", res.ETag)
	}
	httpx.WriteRawJSON(w, res.Status, res.Body)
}

func (s *Service) handleTemplates(w http.ResponseWriter, r *http.Request) error {
	httpx.WriteJSON(w, http.StatusOK, s.ListTemplates())
	return nil
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	lq := ListQuery{Q: q.Get("q"), Filter: q.Get("filter"), Cursor: q.Get("cursor")}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return httpx.Validation("limit должен быть числом от 1 до 100.", "limit")
		}
		lq.Limit = n
	}
	page, err := s.List(r.Context(), subject(r), lq)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, page)
	return nil
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) error {
	key, err := idempotency.Key(r)
	if err != nil {
		return err
	}
	var in CreateScenario
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	res, err := s.Create(r.Context(), subject(r), key, in)
	if err != nil {
		return err
	}
	write(w, res)
	return nil
}

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) error {
	res, err := s.Get(r.Context(), subject(r), r.PathValue("scenarioId"))
	if err != nil {
		return err
	}
	write(w, res)
	return nil
}

func (s *Service) handleSaveDraft(w http.ResponseWriter, r *http.Request) error {
	version, err := ParseIfMatch(r.Header.Get("If-Match"))
	if err != nil {
		return err
	}
	var in SaveDraft
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	res, err := s.SaveDraft(r.Context(), subject(r), r.PathValue("scenarioId"), version, in.Plan)
	if err != nil {
		return err
	}
	write(w, res)
	return nil
}

func (s *Service) handlePublish(w http.ResponseWriter, r *http.Request) error {
	key, err := idempotency.Key(r)
	if err != nil {
		return err
	}
	version, err := ParseIfMatch(r.Header.Get("If-Match"))
	if err != nil {
		return err
	}
	var in Publish
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	res, err := s.Publish(r.Context(), subject(r), r.PathValue("scenarioId"), key, version, in)
	if err != nil {
		return err
	}
	write(w, res)
	return nil
}
