-- Identity and assignments (module identity, owner: Бэк-2).
-- Until SSO is agreed, users come from the demo seed.

CREATE TABLE users (
  id            text PRIMARY KEY,
  display_name  text NOT NULL
);

-- An assignment ties one intern to a buddy, a lead and a team.
CREATE TABLE assignments (
  id         text PRIMARY KEY,
  intern_id  text NOT NULL REFERENCES users(id),
  buddy_id   text NOT NULL REFERENCES users(id),
  lead_id    text NOT NULL REFERENCES users(id),
  team_id    text NOT NULL
);
CREATE INDEX assignments_intern_idx ON assignments(intern_id);
CREATE INDEX assignments_buddy_idx  ON assignments(buddy_id);
CREATE INDEX assignments_lead_idx   ON assignments(lead_id);

-- Shared by all modules: results of requests with Idempotency-Key.
-- Retention period is an open question (server-handoff.md); rows are never deleted for now.
CREATE TABLE idempotency_records (
  subject       text NOT NULL,
  operation     text NOT NULL,
  key           text NOT NULL,
  payload_hash  text NOT NULL,
  status        int  NOT NULL,
  response      jsonb NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (subject, operation, key)
);
