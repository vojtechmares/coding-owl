-- What Verification said about a Run (ADR-0013, ADR-0030). Every check runs,
-- and every one of them is recorded, so a blocked Job can report everything
-- that is wrong at once rather than one thing a morning.
CREATE TABLE check_results (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id    INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    -- position keeps the order the checks were configured in, which is the
    -- order a report reads best in.
    position  INTEGER NOT NULL,
    name      TEXT NOT NULL,
    command   TEXT NOT NULL,
    passed    INTEGER NOT NULL,
    exit_code INTEGER NOT NULL DEFAULT -1,
    output    TEXT NOT NULL DEFAULT '',
    -- reason is why it failed, in the words the user reads; empty when it
    -- passed.
    reason    TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX check_results_by_run ON check_results (run_id, position);

-- Why a Job is where it is when no Run explains it: a setup command that
-- failed before an Agent started, or the checks that refused the work.
ALTER TABLE jobs ADD COLUMN note TEXT NOT NULL DEFAULT '';
