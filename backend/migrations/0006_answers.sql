-- Personal questions to Хомыч and their answers (module answers).
-- A question and its answer are messages of the same feed as the calendar checkpoints, so the
-- intern sees one history on any device. Only the intern of the assignment receives them.

ALTER TABLE messages
  ALTER COLUMN event_id DROP NOT NULL,
  ALTER COLUMN block DROP NOT NULL,
  ALTER COLUMN scenario_id DROP NOT NULL,
  ALTER COLUMN revision_id DROP NOT NULL,
  ADD COLUMN citations jsonb,               -- assistant: sources the answer is based on
  ADD COLUMN answer_request_id text,        -- user and assistant: the AnswerJob
  ADD COLUMN error_code text,               -- assistant: the answer could not be produced
  ADD CONSTRAINT messages_kind_check CHECK (kind IN ('checkpoint', 'user', 'assistant')),
  ADD CONSTRAINT messages_checkpoint_fields CHECK (
    kind <> 'checkpoint' OR (event_id IS NOT NULL AND block IS NOT NULL AND scenario_id IS NOT NULL AND revision_id IS NOT NULL));

-- The durable job behind a question: stored before the knowledge service is called,
-- claimed by the worker with a lease, retried after a failure, completed exactly once.
CREATE TABLE answer_jobs (
  id                   text PRIMARY KEY,
  assignment_id        text NOT NULL REFERENCES assignments(id),
  author_id            text NOT NULL REFERENCES users(id),
  question_message_id  text NOT NULL UNIQUE REFERENCES messages(id),
  question             text NOT NULL,
  status               text NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed')),
  attempts             int  NOT NULL DEFAULT 0,
  available_at         timestamptz NOT NULL,   -- not before: backoff after a failed attempt
  lease_until          timestamptz,            -- a running job whose lease expired is taken again
  answer_message_id    text REFERENCES messages(id),
  error_code           text,
  outcome              text,                   -- answered | no_evidence | conflicting_sources
  retrieval_mode       text,
  snapshot_id          text,
  created_at           timestamptz NOT NULL,
  updated_at           timestamptz NOT NULL
);
CREATE INDEX answer_jobs_pending_idx ON answer_jobs(available_at) WHERE status IN ('queued', 'running');
CREATE INDEX answer_jobs_author_idx ON answer_jobs(author_id, created_at DESC);
