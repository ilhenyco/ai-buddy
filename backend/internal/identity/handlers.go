package identity

import (
	"net/http"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
)

// Routes registers GET /session/context (contract) and, in demo mode,
// the demo login used instead of SSO:
//
//	GET    /demo/users    list of test users for the login screen
//	POST   /demo/session  {"userId":"u-alex"} -> sets the cookie, returns Context
//	DELETE /demo/session  logout
func (s *Service) Routes(mux *http.ServeMux, prefix string, demo bool) {
	mux.Handle("GET "+prefix+"/session/context", s.Authenticate(httpx.Handler(s.handleContext)))
	if !demo {
		return
	}
	mux.Handle("GET "+prefix+"/demo/users", httpx.Handler(s.handleDemoUsers))
	mux.Handle("POST "+prefix+"/demo/session", httpx.Handler(s.handleDemoLogin))
	mux.Handle("DELETE "+prefix+"/demo/session", httpx.Handler(s.handleLogout))
}

func (s *Service) handleContext(w http.ResponseWriter, r *http.Request) error {
	p, _ := Subject(r.Context())
	cookie, _ := r.Context().Value(cookieKey).(string)
	c, err := s.BuildContext(r.Context(), p, cookie)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, c)
	return nil
}

func (s *Service) handleDemoUsers(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.QueryContext(r.Context(), `SELECT id, display_name FROM users ORDER BY display_name`)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Person{}
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.DisplayName); err != nil {
			return err
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

func (s *Service) handleDemoLogin(w http.ResponseWriter, r *http.Request) error {
	if err := s.checkOrigin(r); err != nil {
		return err
	}
	var body struct {
		UserID string `json:"userId"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	p, err := s.Person(r.Context(), body.UserID)
	if err != nil {
		return httpx.Validation("Неизвестный тестовый пользователь.", "userId")
	}
	value := s.NewSessionValue(p.ID)
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: value, Path: "/", HttpOnly: true,
		Secure: s.SecureCookie, SameSite: http.SameSiteLaxMode, MaxAge: int(SessionTTL.Seconds())})
	c, err := s.BuildContext(r.Context(), p, value)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, c)
	return nil
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) error {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
	return nil
}
