package answers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Knowledge is the Python RAG service (knowledge/): it searches the corpus with the subject's
// rights and writes an answer with citations. It keeps no history: Go stores questions and answers.
type Knowledge interface {
	Answer(ctx context.Context, in RAGInput) (RAGResult, error)
}

// RAGInput is POST /internal/v1/answer of knowledge/contracts/openapi.json.
type RAGInput struct {
	SubjectID      string  `json:"subjectId"`
	RequestID      string  `json:"requestId"`
	Query          string  `json:"query"`
	MessageContext *string `json:"messageContext,omitempty"`
}

type RAGCitation struct {
	ChunkID    string `json:"chunkId"`
	DocumentID string `json:"documentId"`
	Revision   string `json:"revision"`
	Title      string `json:"title"`
	Section    string `json:"section"`
	URL        string `json:"url"`
	Synthetic  bool   `json:"synthetic"`
}

// RAGResult: answered | no_evidence | conflicting_sources are outcomes to show;
// unavailable: the service could not prepare an answer; the job fails with an honest message.
type RAGResult struct {
	RequestID     string        `json:"requestId"`
	Status        string        `json:"status"`
	Reply         string        `json:"reply"`
	Citations     []RAGCitation `json:"citations"`
	SnapshotID    *string       `json:"snapshotId"`
	RetrievalMode string        `json:"retrievalMode"`
}

// RejectedError: the service refused the request (401/403/404/422); repeating will not help.
type RejectedError struct{ Status int }

func (e *RejectedError) Error() string {
	return fmt.Sprintf("knowledge service rejected the request: %d", e.Status)
}

// HTTPKnowledge calls the service with the internal credential. The timeout must exceed the
// service's own request budget (RAG_REQUEST_TIMEOUT), otherwise every slow answer is cut off.
type HTTPKnowledge struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func NewHTTPKnowledge(baseURL, token string, timeout time.Duration) *HTTPKnowledge {
	return &HTTPKnowledge{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

func (k *HTTPKnowledge) Answer(ctx context.Context, in RAGInput) (RAGResult, error) {
	var out RAGResult
	b, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.BaseURL+"/internal/v1/answer", bytes.NewReader(b))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+k.Token)
	resp, err := k.HTTP.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 || resp.StatusCode == 422:
		return out, &RejectedError{Status: resp.StatusCode}
	default:
		return out, fmt.Errorf("knowledge service: %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("knowledge service: %w", err)
	}
	if out.RequestID != in.RequestID {
		return out, errors.New("knowledge service answered another request")
	}
	return out, nil
}
