-- Jobs queued against a Project. A Job is a standing intent to do one piece of
-- work (ADR-0027). source and source_ref are unique together, which is what
-- makes producing a Job an upsert rather than a second Job (ADR-0032).
--
-- position is the Job's place in the queue and is NULL for a Job that is not
-- in it. Order is the whole schedule (ADR-0025), so it is stored rather than
-- derived, and there is no priority column to derive it from.
CREATE TABLE jobs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    source     TEXT NOT NULL,
    source_ref TEXT NOT NULL,
    project    TEXT NOT NULL REFERENCES projects(name) ON UPDATE CASCADE ON DELETE CASCADE,
    prompt     TEXT NOT NULL,
    state      TEXT NOT NULL,
    position   INTEGER,
    created    TEXT NOT NULL,
    UNIQUE (source, source_ref)
) STRICT;

-- The scheduler and owl queue list both read the queue in order.
CREATE INDEX jobs_queue ON jobs (position) WHERE position IS NOT NULL;
