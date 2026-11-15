package metrics_test

import (
	"strings"
	"testing"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/metrics"
)

func TestOutboxMetrics(t *testing.T) {
	m := metrics.New()
	m.OutboxPublished("stage.delivered")
	m.OutboxPublished("stage.delivered")
	m.OutboxFailed()
	m.OutboxPending(7)
	out := scrape(t, m)
	for _, want := range []string{`homych_outbox_published_total{type="stage.delivered"} 2`, "homych_outbox_publish_failures_total 1", "homych_outbox_pending 7"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	var none *metrics.Metrics
	none.OutboxPublished("x")
	none.OutboxFailed()
	none.OutboxPending(1)
}
