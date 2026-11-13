// Package accessmock imitates Avito's access request service for the demo (OQ08).
// It runs as a separate container (cmd/mock-access) and is replaced by the real service
// without changing the access module: only ACCESS_SERVICE_URL changes.
//
//	POST /requests                  {"correlationId","userId","resourceId","reason"} -> {"number","status"}
//	                                idempotent by correlationId: a repeat returns the same request
//	GET  /requests/{correlationId}  the request or 404
//	POST /admin/mode                {"mode":"approve"|"deny"|"lost"} demo switch
//
// A new request is "pending"; after ApproveAfter it becomes "approved" ("denied" in deny mode).
// In "lost" mode the request is stored but the answer never comes back in time,
// which shows how the caller recovers an unknown outcome by correlationId.
package accessmock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/clock"
)

type Request struct {
	CorrelationID string    `json:"correlationId"`
	UserID        string    `json:"userId"`
	ResourceID    string    `json:"resourceId"`
	Reason        string    `json:"reason"`
	Number        string    `json:"number"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
	decision      string
}

type Mock struct {
	Clock        clock.Clock
	ApproveAfter time.Duration
	// LostDelay is how long a "lost" answer hangs before the connection is closed without a body.
	LostDelay time.Duration

	mu    sync.Mutex
	mode  string
	seq   int
	byKey map[string]*Request
	calls int
}

func New(clk clock.Clock, approveAfter time.Duration) *Mock {
	return &Mock{Clock: clk, ApproveAfter: approveAfter, LostDelay: 10 * time.Second, mode: "approve", seq: 1041, byKey: map[string]*Request{}}
}

// Calls is the number of POST /requests (tests check that a retry does not submit twice).
func (m *Mock) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *Mock) SetMode(mode string) error {
	if mode != "approve" && mode != "deny" && mode != "lost" {
		return fmt.Errorf("unknown mode %q", mode)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mode = mode
	return nil
}

// view returns the request with its status at the current time.
func (m *Mock) view(r *Request) Request {
	out := *r
	if out.Status == "pending" && m.Clock.Now().Sub(r.CreatedAt) >= m.ApproveAfter {
		out.Status = r.decision
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (m *Mock) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /requests", func(w http.ResponseWriter, r *http.Request) {
		var in Request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.CorrelationID == "" || in.ResourceID == "" || in.UserID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "correlationId, userId and resourceId are required"})
			return
		}
		m.mu.Lock()
		m.calls++
		req, ok := m.byKey[in.CorrelationID]
		if !ok {
			m.seq++
			decision := "approved"
			if m.mode == "deny" {
				decision = "denied"
			}
			req = &Request{CorrelationID: in.CorrelationID, UserID: in.UserID, ResourceID: in.ResourceID, Reason: in.Reason,
				Number: fmt.Sprintf("MOCK-%d", m.seq), Status: "pending", CreatedAt: m.Clock.Now(), decision: decision}
			m.byKey[in.CorrelationID] = req
		}
		lost := m.mode == "lost"
		out := m.view(req)
		m.mu.Unlock()
		if lost {
			// stored, but the answer is lost: the caller times out first
			select {
			case <-time.After(m.LostDelay):
			case <-r.Context().Done():
			}
			return
		}
		writeJSON(w, http.StatusCreated, out)
	})
	mux.HandleFunc("GET /requests/{id}", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		req, ok := m.byKey[r.PathValue("id")]
		var out Request
		if ok {
			out = m.view(req)
		}
		m.mu.Unlock()
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("POST /admin/mode", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || m.SetMode(in.Mode) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode: approve | deny | lost"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"mode": in.Mode})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}
