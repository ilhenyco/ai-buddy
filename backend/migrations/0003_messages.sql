-- Messages by the calendar (module messages).
-- Rules from docs/ai-buddy/server-handoff.md:
--   one event per (assignment, scenario, block) — republishing the same variant never repeats a sent block;
--   one delivery per (event, recipient, role); the text is a snapshot taken at send time;
--   checks never influence the calendar.

-- A due block of the active plan of an assignment.
CREATE TABLE message_events (
  id             text PRIMARY KEY,
  assignment_id  text NOT NULL REFERENCES assignments(id),
  scenario_id    text NOT NULL REFERENCES scenarios(id),
  revision_id    text NOT NULL REFERENCES scenario_revisions(id),
  block_id       text NOT NULL,
  block_date     date NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (assignment_id, scenario_id, block_id)
);

-- One message per recipient of an event: intern, buddy and lead get their own text.
CREATE TABLE messages (
  id                 text PRIMARY KEY,
  event_id           text NOT NULL REFERENCES message_events(id),
  assignment_id      text NOT NULL REFERENCES assignments(id),
  recipient_user_id  text NOT NULL REFERENCES users(id),
  recipient_role     text NOT NULL CHECK (recipient_role IN ('intern', 'buddy', 'lead')),
  kind               text NOT NULL DEFAULT 'checkpoint',
  text               text NOT NULL,
  block              jsonb NOT NULL,      -- snapshot of the block at send time
  next_block         jsonb,               -- {id, title, date} of the following block, if any
  scenario_id        text NOT NULL,
  revision_id        text NOT NULL,
  created_at         timestamptz NOT NULL,
  UNIQUE (event_id, recipient_user_id, recipient_role)
);
CREATE INDEX messages_feed_idx ON messages(assignment_id, recipient_user_id, created_at, id);
CREATE INDEX messages_recipient_idx ON messages(recipient_user_id, created_at DESC);

-- A formal "done" mark of the recipient. It is only recorded, never read by the scheduler.
CREATE TABLE message_checks (
  message_id  text NOT NULL REFERENCES messages(id),
  task_id     text NOT NULL,
  user_id     text NOT NULL REFERENCES users(id),
  checked     boolean NOT NULL,
  updated_at  timestamptz NOT NULL,
  PRIMARY KEY (message_id, task_id, user_id)
);
