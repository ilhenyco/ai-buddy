package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// LogPublisher writes each event as one structured log line ("event" message). It is the
// default: the events are visible in `docker compose logs api` without any infrastructure.
type LogPublisher struct{ Log *slog.Logger }

func (p LogPublisher) Publish(_ context.Context, events []Envelope) error {
	for _, e := range events {
		attrs := []any{"id", e.ID, "type", e.Type, "aggregate", e.Aggregate.Type + "/" + e.Aggregate.ID,
			"occurredAt", e.OccurredAt, "data", e.Data}
		if e.AssignmentID != nil {
			attrs = append(attrs, "assignmentId", *e.AssignmentID)
		}
		if e.TeamID != nil {
			attrs = append(attrs, "teamId", *e.TeamID)
		}
		if e.Actor != nil {
			attrs = append(attrs, "actor", e.Actor.UserID, "actorRole", e.Actor.Role)
		}
		p.Log.Info("event", attrs...)
	}
	return nil
}

// HTTPPublisher POSTs a batch as {"events":[…]} to a collector (Clickstream, a webhook, a
// Kafka REST proxy). Any 2xx is success; the X-Batch-Id header is the first event id.
type HTTPPublisher struct {
	URL    string
	Token  string // optional: Authorization: Bearer
	Client *http.Client
}

func (p HTTPPublisher) Publish(ctx context.Context, events []Envelope) error {
	body, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if len(events) > 0 {
		req.Header.Set("X-Batch-Id", events[0].ID)
	}
	if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	c := p.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("collector answered %d", resp.StatusCode)
	}
	return nil
}
