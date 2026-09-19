-- A Job may name another Job it waits for: the scheduler passes it over until
-- that one is done (ADR-0025), and its Agent is told what it is waiting on.
-- Zero is no dependency, which is what almost every Job is, so it is the
-- default rather than a value anybody writes. No foreign key: the store opens
-- with foreign_keys on and 0 is not a Job id, so every existing row would
-- violate one. queue.Add checks the Job exists instead, where the refusal can
-- be a sentence a person reads.
ALTER TABLE jobs ADD COLUMN blocked_by INTEGER NOT NULL DEFAULT 0;
