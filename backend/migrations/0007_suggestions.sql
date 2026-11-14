-- Question suggestions of the current stage (module suggestions): one stored triple per assignment.
-- It changes only when the stage changes, so the buttons stay the same after answers and reloads.
CREATE TABLE suggestion_selections (
  assignment_id  text PRIMARY KEY REFERENCES assignments(id),
  stage_key      text NOT NULL,
  stage_kind     text NOT NULL,
  pool_version   text NOT NULL,
  questions      jsonb NOT NULL,
  selected_at    timestamptz NOT NULL
);
