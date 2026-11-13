package messages

import (
	"net/http"
	"strconv"

	"github.com/ilhenyco/ai-buddy/backend/internal/identity"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
)

// Routes registers the contract operations and, in demo mode, the demo clock:
//
//	GET  /demo/clock  {"today":"2026-11-12","shifted":false}
//	POST /demo/clock  {"date":"2026-11-19"} moves "today", {"date":null} returns to real time;
//	                  the scheduler runs right away and the answer says how many messages it created.
func (s *Service) Routes(mux *http.ServeMux, prefix string, auth func(http.Handler) http.Handler, demo *clock.Offset) {
	h := func(pattern string, fn httpx.Handler) { mux.Handle(pattern, auth(fn)) }
	h("GET "+prefix+"/assignments/{assignmentId}/messages", s.handleFeed)
	h("PUT "+prefix+"/messages/{messageId}/tasks/{taskId}/check", s.handleCheck)
	h("GET "+prefix+"/notifications", s.handleNotifications)
	if demo == nil {
		return
	}
	h("GET "+prefix+"/demo/clock", func(w http.ResponseWriter, r *http.Request) error {
		httpx.WriteJSON(w, http.StatusOK, ClockState{Today: s.today(), Shifted: demo.Shifted()})
		return nil
	})
	h("POST "+prefix+"/demo/clock", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Date *string `json:"date"`
		}
		if err := httpx.DecodeJSON(r, &in); err != nil {
			return err
		}
		if in.Date == nil {
			demo.Reset()
		} else if err := demo.SetToday(*in.Date, s.Loc); err != nil {
			return httpx.Validation("Дата в формате ГГГГ-ММ-ДД.", "date")
		}
		n, err := s.RunOnce(r.Context())
		if err != nil {
			return err
		}
		httpx.WriteJSON(w, http.StatusOK, ClockState{Today: s.today(), Shifted: demo.Shifted(), Created: n})
		return nil
	})
}

func subject(r *http.Request) string {
	p, _ := identity.Subject(r.Context())
	return p.ID
}

func limitParam(r *http.Request) (int, error) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, httpx.Validation("limit должен быть положительным числом.", "limit")
	}
	return n, nil
}

func (s *Service) handleFeed(w http.ResponseWriter, r *http.Request) error {
	limit, err := limitParam(r)
	if err != nil {
		return err
	}
	page, err := s.Feed(r.Context(), subject(r), r.PathValue("assignmentId"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, page)
	return nil
}

func (s *Service) handleCheck(w http.ResponseWriter, r *http.Request) error {
	var in SetCheck
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if in.Checked == nil {
		return httpx.Validation("Поле checked обязательно.", "checked")
	}
	c, err := s.SetCheck(r.Context(), subject(r), r.PathValue("messageId"), r.PathValue("taskId"), *in.Checked)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, c)
	return nil
}

func (s *Service) handleNotifications(w http.ResponseWriter, r *http.Request) error {
	limit, err := limitParam(r)
	if err != nil {
		return err
	}
	page, err := s.Notifications(r.Context(), subject(r), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, page)
	return nil
}
