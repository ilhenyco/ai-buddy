package identity_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"context"
	"github.com/ilhenyco/ai-buddy/backend/internal/app"
	"github.com/ilhenyco/ai-buddy/backend/internal/identity"
	"github.com/ilhenyco/ai-buddy/backend/internal/platform/testdb"
)

func TestAIContextAuth(t *testing.T) {
	s := &identity.Service{}
	mux := http.NewServeMux()
	s.AIRoutes(mux, "a-long-dedicated-test-service-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/internal/v1/ai-context", bytes.NewBufferString(`{"subjectId":"u-sasha"}`)))
	if w.Code != 401 {
		t.Fatalf("want 401, got %d", w.Code)
	}
	disabled := http.NewServeMux()
	s.AIRoutes(disabled, "")
	w = httptest.NewRecorder()
	disabled.ServeHTTP(w, httptest.NewRequest("POST", "/internal/v1/ai-context", nil))
	if w.Code != 404 {
		t.Fatalf("disabled route: %d", w.Code)
	}
}

func TestAIContextAssignments(t *testing.T) {
	d := testdb.Open(t)
	if err := app.Prepare(context.Background(), d, true); err != nil {
		t.Fatal(err)
	}
	s := &identity.Service{DB: d}
	mux := http.NewServeMux()
	const token = "a-long-dedicated-test-service-token"
	s.AIRoutes(mux, token)
	for _, tc := range []struct {
		body   string
		status int
		team   string
	}{
		{`{"subjectId":"u-sasha"}`, 200, "platform"},
		{`{"subjectId":"u-kirill"}`, 200, "mobile"},
		{`{"subjectId":"u-alex"}`, 403, ""},
		{`{"subjectId":"u-dasha"}`, 403, ""},
		{`{"subjectId":"u-anna"}`, 403, ""},
		{`{"subjectId":"missing"}`, 404, ""},
		{`{"subjectId":"u-sasha","teamIds":["mobile"]}`, 422, ""},
		{`{"subjectId":""}`, 422, ""},
	} {
		r := httptest.NewRequest("POST", "/internal/v1/ai-context", bytes.NewBufferString(tc.body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: want %d got %d %s", tc.body, tc.status, w.Code, w.Body.String())
		}
		if tc.status == 200 {
			var out identity.AIContext
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if len(out.TeamIDs) != 1 || out.TeamIDs[0] != tc.team || out.Synthetic {
				t.Fatalf("wrong ACL: %+v", out)
			}
		}
	}
	if _, err := d.Exec("DELETE FROM assignments WHERE intern_id='u-sasha'"); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/internal/v1/ai-context", bytes.NewBufferString(`{"subjectId":"u-sasha"}`))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("revoked assignment: %d", w.Code)
	}
}
