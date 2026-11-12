-- Scenarios and publications (module scenarios).
-- Contract with the scheduler (Бэк-1): read active_scenarios + scenario_revisions.plan,
-- send only blocks with date > cutoff_date (when cutoff_date is set),
-- deduplicate by (assignment_id, scenario_id, block_id).

-- A scenario is a working draft owned by a lead. Several variants per intern are allowed.
CREATE TABLE scenarios (
  id                   text PRIMARY KEY,
  owner_id             text NOT NULL REFERENCES users(id),
  assignment_id        text REFERENCES assignments(id),   -- NULL until an intern is chosen
  draft                jsonb NOT NULL,                     -- may be incomplete
  draft_version        int  NOT NULL DEFAULT 1 CHECK (draft_version >= 1),
  latest_revision_id   text,                               -- last publication of this variant
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX scenarios_owner_idx ON scenarios(owner_id, updated_at DESC);

-- Published versions are immutable.
CREATE TABLE scenario_revisions (
  id            text PRIMARY KEY,
  scenario_id   text NOT NULL REFERENCES scenarios(id),
  number        int  NOT NULL CHECK (number >= 1),
  plan          jsonb NOT NULL,
  published_by  text NOT NULL REFERENCES users(id),
  published_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (scenario_id, number)
);

ALTER TABLE scenarios
  ADD CONSTRAINT scenarios_latest_revision_fk FOREIGN KEY (latest_revision_id) REFERENCES scenario_revisions(id);

-- Exactly one active (sending) variant per assignment.
CREATE TABLE active_scenarios (
  assignment_id  text PRIMARY KEY REFERENCES assignments(id),
  scenario_id    text NOT NULL REFERENCES scenarios(id),
  revision_id    text NOT NULL REFERENCES scenario_revisions(id),
  cutoff_date    date,          -- after a replacement: do not send blocks with date <= cutoff_date
  activated_at   timestamptz NOT NULL DEFAULT now()
);
