-- Team knowledge sources (module sources): the lead adds and edits materials, the knowledge
-- service indexes them. Every edit is a new immutable version; until the new version is indexed,
-- answers keep using the previous one (active_version_id).

CREATE TABLE sources (
  id                 text PRIMARY KEY,
  team_id            text NOT NULL,                 -- acl.teamIds[0]: who may read it in answers
  category           text NOT NULL,
  title              text NOT NULL,
  latest_version_id  text NOT NULL,
  active_version_id  text,                          -- the version answers are based on; NULL before the first index
  excluded           boolean NOT NULL DEFAULT false,
  etag               int NOT NULL DEFAULT 1 CHECK (etag >= 1),
  created_by         text NOT NULL REFERENCES users(id),
  created_at         timestamptz NOT NULL,
  updated_at         timestamptz NOT NULL
);
CREATE INDEX sources_team_idx ON sources(team_id, updated_at DESC, id);

CREATE TABLE source_versions (
  id          text PRIMARY KEY,
  source_id   text NOT NULL REFERENCES sources(id),
  title       text NOT NULL,
  body        text NOT NULL,
  category    text NOT NULL,
  acl         jsonb NOT NULL,
  author_id   text NOT NULL REFERENCES users(id),
  created_at  timestamptz NOT NULL
);

-- Indexing of a version (or removal from the index after exclusion) in the knowledge service.
CREATE TABLE source_jobs (
  id                    text PRIMARY KEY,
  source_id             text NOT NULL REFERENCES sources(id),
  candidate_version_id  text NOT NULL REFERENCES source_versions(id),
  action                text NOT NULL CHECK (action IN ('index', 'remove')),
  status                text NOT NULL CHECK (status IN ('queued', 'running', 'ready', 'failed')),
  external_job_id       text,          -- the knowledge service's ingestion job
  attempts              int NOT NULL DEFAULT 0,
  available_at          timestamptz NOT NULL,
  error_code            text,
  created_at            timestamptz NOT NULL,
  updated_at            timestamptz NOT NULL
);
CREATE INDEX source_jobs_pending_idx ON source_jobs(available_at) WHERE status IN ('queued', 'running');
