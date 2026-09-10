-- A Run is one attempt to carry out a Job (ADR-0027). A Job may take several,
-- so the worktree and the branch belong to the Job (ADR-0007) and are recorded
-- on it, while what happened on one night is recorded here.
ALTER TABLE jobs ADD COLUMN branch TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN worktree TEXT NOT NULL DEFAULT '';

CREATE TABLE runs (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id   INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    attempt  INTEGER NOT NULL,
    started  TEXT NOT NULL,
    -- ended and outcome are empty while the Run is still going.
    ended    TEXT NOT NULL DEFAULT '',
    outcome  TEXT NOT NULL DEFAULT '',
    error    TEXT NOT NULL DEFAULT '',
    log_path TEXT NOT NULL
) STRICT;

-- owl jobs show reads a Job's attempts in order, and the scheduler asks
-- whether anything is running at all.
CREATE INDEX runs_by_job ON runs (job_id, attempt);
CREATE INDEX runs_in_progress ON runs (id) WHERE outcome = '';
