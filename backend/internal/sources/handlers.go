package sources

import (
	"net/http"
	"strconv"

	"github.com/ilhenyco/ai-buddy/backend/internal/identity"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/idempotency"
	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
)

func (s *Service) Routes(mux *http.ServeMux, prefix string, auth func(http.Handler) http.Handler) {
	h := func(pattern string, fn httpx.Handler) { mux.Handle(pattern, auth(fn)) }
	h("GET "+prefix+"/sources", func(w http.ResponseWriter, r *http.Request) error {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		page, err := s.List(r.Context(), subject(r), r.URL.Query().Get("q"), r.URL.Query().Get("cursor"), limit)
		if err != nil {
			return err
		}
		httpx.WriteJSON(w, http.StatusOK, page)
		return nil
	})
	h("POST "+prefix+"/sources", func(w http.ResponseWriter, r *http.Request) error {
		key, err := idempotency.Key(r)
		if err != nil {
			return err
		}
		var in Input
		if err := httpx.DecodeJSON(r, &in); err != nil {
			return err
		}
		status, body, err := s.Create(r.Context(), subject(r), key, in)
		return write(w, status, body, err)
	})
	h("GET "+prefix+"/sources/{sourceId}", func(w http.ResponseWriter, r *http.Request) error {
		src, err := s.Get(r.Context(), subject(r), r.PathValue("sourceId"), r.URL.Query().Get("versionId"))
		if err != nil {
			return err
		}
		w.Header().Set("ETag", `"`+src.ETag+`"`)
		httpx.WriteJSON(w, http.StatusOK, src)
		return nil
	})
	h("PUT "+prefix+"/sources/{sourceId}", func(w http.ResponseWriter, r *http.Request) error {
		key, etag, err := keyAndETag(r)
		if err != nil {
			return err
		}
		var in Input
		if err := httpx.DecodeJSON(r, &in); err != nil {
			return err
		}
		status, body, err := s.Update(r.Context(), subject(r), r.PathValue("sourceId"), key, etag, in)
		return write(w, status, body, err)
	})
	h("POST "+prefix+"/sources/{sourceId}/exclude", func(w http.ResponseWriter, r *http.Request) error {
		key, etag, err := keyAndETag(r)
		if err != nil {
			return err
		}
		status, body, err := s.Exclude(r.Context(), subject(r), r.PathValue("sourceId"), key, etag)
		return write(w, status, body, err)
	})
	h("GET "+prefix+"/source-jobs/{jobId}", func(w http.ResponseWriter, r *http.Request) error {
		j, err := s.GetJob(r.Context(), subject(r), r.PathValue("jobId"))
		if err != nil {
			return err
		}
		httpx.WriteJSON(w, http.StatusOK, j)
		return nil
	})
}

func subject(r *http.Request) string {
	p, _ := identity.Subject(r.Context())
	return p.ID
}

func keyAndETag(r *http.Request) (string, int, error) {
	key, err := idempotency.Key(r)
	if err != nil {
		return "", 0, err
	}
	etag, err := scenarios.ParseIfMatch(r.Header.Get("If-Match"))
	return key, etag, err
}

func write(w http.ResponseWriter, status int, body []byte, err error) error {
	if err != nil {
		return err
	}
	httpx.WriteRawJSON(w, status, body)
	return nil
}
