package access

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

// External is the access request service of Avito. In the demo it is cmd/mock-access;
// the real service replaces it by changing ACCESS_SERVICE_URL.
type External interface {
	Submit(ctx context.Context, r ExternalRequest) (ExternalRequest, error)
	// Lookup returns found=false when the service has no request with this correlationId.
	Lookup(ctx context.Context, correlationID string) (ExternalRequest, bool, error)
	SetMode(ctx context.Context, mode string) error
}

type ExternalRequest struct {
	CorrelationID string `json:"correlationId"`
	UserID        string `json:"userId"`
	ResourceID    string `json:"resourceId"`
	Reason        string `json:"reason"`
	Number        string `json:"number,omitempty"`
	Status        string `json:"status,omitempty"`
}

// HTTPClient talks to the service over HTTP with a short timeout: a slow or lost answer
// must not block the interface, the outcome is reconciled later.
type HTTPClient struct {
	BaseURL string
	HTTP    *http.Client
}

func NewHTTPClient(baseURL string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: timeout}}
}

var errUnexpected = errors.New("unexpected answer of the access service")

func (c *HTTPClient) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("%w: %v", errUnexpected, err)
		}
	}
	return resp.StatusCode, nil
}

func (c *HTTPClient) Submit(ctx context.Context, r ExternalRequest) (ExternalRequest, error) {
	var out ExternalRequest
	code, err := c.do(ctx, http.MethodPost, "/requests", r, &out)
	if err == nil && code != http.StatusCreated && code != http.StatusOK {
		err = fmt.Errorf("%w: %d", errUnexpected, code)
	}
	return out, err
}

func (c *HTTPClient) Lookup(ctx context.Context, correlationID string) (ExternalRequest, bool, error) {
	var out ExternalRequest
	code, err := c.do(ctx, http.MethodGet, "/requests/"+correlationID, nil, &out)
	switch {
	case err != nil:
		return out, false, err
	case code == http.StatusNotFound:
		return out, false, nil
	case code != http.StatusOK:
		return out, false, fmt.Errorf("%w: %d", errUnexpected, code)
	}
	return out, true, nil
}

func (c *HTTPClient) SetMode(ctx context.Context, mode string) error {
	code, err := c.do(ctx, http.MethodPost, "/admin/mode", map[string]string{"mode": mode}, nil)
	if err == nil && code != http.StatusOK {
		err = fmt.Errorf("%w: %d", errUnexpected, code)
	}
	return err
}
