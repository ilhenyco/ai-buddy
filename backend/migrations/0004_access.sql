-- Access requests (module access, OQ08 profile).
-- server-handoff.md: human confirmation, resource, correlationId, idempotency, number and status are durable.
-- The operation is stored BEFORE the external call; a lost answer becomes "unknown" and is
-- reconciled by correlationId, never submitted as a second request.

CREATE TABLE access_drafts (
  id             text PRIMARY KEY,
  assignment_id  text NOT NULL REFERENCES assignments(id),
  author_id      text NOT NULL REFERENCES users(id),
  resource_id    text NOT NULL,
  reason         text NOT NULL,
  version        int  NOT NULL DEFAULT 1 CHECK (version >= 1),
  expires_at     timestamptz NOT NULL,
  created_at     timestamptz NOT NULL
);

CREATE TABLE access_operations (
  id                    text PRIMARY KEY,
  draft_id              text NOT NULL UNIQUE REFERENCES access_drafts(id),  -- one submission per draft
  assignment_id         text NOT NULL REFERENCES assignments(id),
  requester_id          text NOT NULL REFERENCES users(id),
  resource_id           text NOT NULL,
  reason                text NOT NULL,
  correlation_id        text NOT NULL UNIQUE,
  external_number       text,
  status                text NOT NULL CHECK (status IN ('pending', 'approved', 'denied', 'unknown')),
  submission_confirmed  boolean NOT NULL DEFAULT false,  -- the external service acknowledged the request
  created_at            timestamptz NOT NULL,
  updated_at            timestamptz NOT NULL
);
CREATE INDEX access_operations_assignment_idx ON access_operations(assignment_id, created_at DESC);
