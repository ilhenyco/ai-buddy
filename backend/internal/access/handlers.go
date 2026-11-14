package access

import (
	"net/http"

	"github.com/ilhenyco/ai-buddy/backend/internal/identity"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/idempotency"
)

// Routes registers the contract operations, the per-assignment list (extension) and, in demo mode,
// the switch of the mock's answer:
//
//	POST /demo/access-mode {"mode":"approve"|"deny"|"lost"}
func (s *Service) Routes(mux *http.ServeMux, prefix string, auth func(http.Handler) http.Handler, demo bool) {
	h := func(pattern string, fn httpx.Handler) { mux.Handle(pattern, auth(fn)) }
	h("GET "+prefix+"/access-resources", func(w http.ResponseWriter, r *http.Request) error {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": Resources})
		return nil
	})
	h("POST "+prefix+"/access-drafts", s.handleCreate)
	h("POST "+prefix+"/access-drafts/{draftId}/confirm", s.handleConfirm)
	h("GET "+prefix+"/access-operations/{operationId}", s.handleGet)
	h("GET "+prefix+"/assignments/{assignmentId}/access-operations", s.handleList)
	h("GET "+prefix+"/assignments/{assignmentId}/protected-first-task", s.handleTask)
	if !demo {
		return
	}
	h("POST "+prefix+"/demo/access-mode", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Mode string `json:"mode"`
		}
		if err := httpx.DecodeJSON(r, &in); err != nil {
			return err
		}
		if in.Mode != "approve" && in.Mode != "deny" && in.Mode != "lost" {
			return httpx.Validation("mode: approve, deny или lost.", "mode")
		}
		if err := s.External.SetMode(r.Context(), in.Mode); err != nil {
			return unavailable()
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"mode": in.Mode})
		return nil
	})
}

func unavailable() error {
	return &httpx.Error{Status: http.StatusServiceUnavailable, Code: httpx.CodeUnavailable, Message: "Сервис заявок недоступен. Попробуйте позже."}
}

func subject(r *http.Request) string {
	p, _ := identity.Subject(r.Context())
	return p.ID
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) error {
	key, err := idempotency.Key(r)
	if err != nil {
		return err
	}
	var in DraftInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	status, body, err := s.CreateDraft(r.Context(), subject(r), key, in)
	if err != nil {
		return err
	}
	httpx.WriteRawJSON(w, status, body)
	return nil
}

func (s *Service) handleConfirm(w http.ResponseWriter, r *http.Request) error {
	key, err := idempotency.Key(r)
	if err != nil {
		return err
	}
	var in Confirm
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	status, op, err := s.ConfirmDraft(r.Context(), subject(r), r.PathValue("draftId"), key, in)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, status, op)
	return nil
}

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) error {
	op, err := s.Get(r.Context(), subject(r), r.PathValue("operationId"))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, op)
	return nil
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) error {
	ops, err := s.List(r.Context(), subject(r), r.PathValue("assignmentId"))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": ops})
	return nil
}

func (s *Service) handleTask(w http.ResponseWriter, r *http.Request) error {
	t, err := s.ProtectedTask(r.Context(), subject(r), r.PathValue("assignmentId"))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, t)
	return nil
}
