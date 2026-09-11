-- Every Job carries a TTL: how many Runs it may still take. One is spent at
-- the end of every Run whatever its outcome, and at zero the Job is exhausted
-- rather than pending (ADR-0025). Ten is the default, generous because a Run
-- interrupted by the user opening their laptop costs the same as a failure.
ALTER TABLE jobs ADD COLUMN ttl INTEGER NOT NULL DEFAULT 10;
