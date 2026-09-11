-- Which Skills a Run ran with (ADR-0024). A Skill is instruction the Project
-- gave the Agent, so what an Agent did is only attributable if what it read is
-- recorded: the source, the ref it followed, the commit that resolved to, and
-- the digest of what was actually fetched.
--
-- The rows belong to the Run, not to the Job: a Job's Skills can move between
-- Runs, and each Run says what it had.
CREATE TABLE run_skills (
    run_id  INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    name    TEXT NOT NULL,
    source  TEXT NOT NULL,
    ref     TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    digest  TEXT NOT NULL,
    PRIMARY KEY (run_id, name)
) STRICT;
