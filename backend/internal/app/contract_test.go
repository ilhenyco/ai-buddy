package app_test

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ilhenyco/ai-buddy/backend/internal/apptest"
)

// Every operation of the contract (docs/ai-buddy/system-analysis/openapi.yaml) has a route on the
// server: an unknown route answers 404 from the catch-all, a wrong method 405. Anonymous requests
// reach a real handler and stop at authentication (401) or validation (4xx other than 404/405).
func TestEveryContractOperationHasARoute(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/ai-buddy/system-analysis/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			Extension   bool   `json:"x-extension"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	env := apptest.New(t)
	anon := env.Anonymous()
	param := regexp.MustCompile(`\{[^}]+\}`)
	contract, extensions := 0, 0
	var checked []string
	for path, methods := range doc.Paths {
		for method, op := range methods {
			url := "/api/v1" + param.ReplaceAllString(path, "x-route-check")
			r := anon.Do(t, strings.ToUpper(method), url, map[string]any{}, map[string]string{"Idempotency-Key": "route-check"})
			// a real handler says 404 for an unknown id only after authentication: anonymous gets 401 first.
			// The login itself is public: an unknown test user is its own 404.
			if r.Status == http.StatusNotFound && path != "/demo/session" {
				t.Errorf("%s %s (%s): no route", strings.ToUpper(method), path, op.OperationID)
			}
			if r.Status == http.StatusMethodNotAllowed {
				t.Errorf("%s %s (%s): method not allowed", strings.ToUpper(method), path, op.OperationID)
			}
			if op.Extension {
				extensions++
			} else {
				contract++
			}
			checked = append(checked, op.OperationID)
		}
	}
	sort.Strings(checked)
	if contract != 23 {
		t.Errorf("contract operations: %d, want 23", contract)
	}
	t.Logf("%d contract operations and %d extensions have routes: %v", contract, extensions, checked)
}
