-- What a Run's Agent was allowed to do (ADR-0035). An unattended Agent denies
-- whatever would have prompted, so what it was granted decides what it could
-- change, and what an Agent did is only attributable if that is recorded.
--
-- The rows belong to the Run, not to the Job: the Account's settings file is
-- the user's to edit between Runs, and a Project's own additions can change,
-- so each Run says what it had. position keeps the order the tool read them in.
CREATE TABLE run_permissions (
    run_id   INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    rule     TEXT NOT NULL,
    PRIMARY KEY (run_id, position)
) STRICT;
