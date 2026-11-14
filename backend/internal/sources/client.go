package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Index is the ingestion queue of the knowledge service (knowledge/EXTERNAL.md): PUT and DELETE
// of a document return a job; the service's worker publishes a new corpus snapshot.
type Index interface {
	Put(ctx context.Context, key string, doc Document) (string, error)
	Remove(ctx context.Context, key, documentID string) (string, error)
	// Job returns the status of an ingestion job: pending | processing | completed | failed.
	Job(ctx context.Context, id string) (status string, attempts int, err error)
}

// Document is the knowledge service's Document schema.
type Document struct {
	DocumentID string    `json:"documentId"`
	Revision   string    `json:"revision"`
	Title      string    `json:"title"`
	Text       string    `json:"text"`
	URL        string    `json:"url"`
	Scope      string    `json:"scope"`
	ScopeID    *string   `json:"scopeId"`
	Synthetic  bool      `json:"synthetic"`
	ValidFrom  time.Time `json:"validFrom"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// HTTPIndex uses a client key with the documents:write scope (RAG_EXTERNAL_CLIENTS of the service).
type HTTPIndex struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func NewHTTPIndex(baseURL, token string, timeout time.Duration) *HTTPIndex {
	return &HTTPIndex{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

func (x *HTTPIndex) do(ctx context.Context, method, path, key string, body, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, x.BaseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+x.Token)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := x.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("knowledge index: %s %s: %d", method, path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (x *HTTPIndex) Put(ctx context.Context, key string, doc Document) (string, error) {
	var out struct {
		JobID string `json:"jobId"`
	}
	err := x.do(ctx, http.MethodPut, "/v1/documents/"+doc.DocumentID, key, doc, &out)
	return out.JobID, err
}

func (x *HTTPIndex) Remove(ctx context.Context, key, documentID string) (string, error) {
	var out struct {
		JobID string `json:"jobId"`
	}
	err := x.do(ctx, http.MethodDelete, "/v1/documents/"+documentID, key, nil, &out)
	return out.JobID, err
}

func (x *HTTPIndex) Job(ctx context.Context, id string) (string, int, error) {
	var out struct {
		Status   string `json:"status"`
		Attempts int    `json:"attempts"`
	}
	err := x.do(ctx, http.MethodGet, "/v1/jobs/"+id, "", nil, &out)
	return out.Status, out.Attempts, err
}
