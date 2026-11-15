-- Business events (platform/outbox): written in the same transaction as the change, published
-- afterwards by the relay at least once. Consumers deduplicate by id. No texts of questions,
-- answers or materials: identifiers and outcomes only (docs/architecture/events.md).

CREATE TABLE outbox_events (
  id              text PRIMARY KEY,
  type            text NOT NULL,
  version         int  NOT NULL DEFAULT 1,
  aggregate_type  text NOT NULL,
  aggregate_id    text NOT NULL,
  assignment_id   text,
  team_id         text,
  actor_id        text,
  actor_role      text,
  payload         jsonb NOT NULL DEFAULT '{}',
  occurred_at     timestamptz NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  published_at    timestamptz,
  attempts        int NOT NULL DEFAULT 0,
  available_at    timestamptz NOT NULL,
  last_error      text
);
-- the relay reads only what is not published yet
CREATE INDEX outbox_events_pending_idx ON outbox_events(available_at, created_at, id) WHERE published_at IS NULL;
CREATE INDEX outbox_events_aggregate_idx ON outbox_events(aggregate_type, aggregate_id, occurred_at);
