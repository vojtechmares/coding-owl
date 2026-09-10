-- A Job is planned before it is executed, and continuity between its Runs is
-- the handoff document on its branch rather than a conversation (ADR-0026).
-- The plan is kept here as well as committed, so owl jobs show can print it
-- without reading the worktree.
ALTER TABLE jobs ADD COLUMN planned INTEGER NOT NULL DEFAULT 1;
ALTER TABLE jobs ADD COLUMN plan TEXT NOT NULL DEFAULT '';

-- Model and effort are chosen per phase and cascade global to Project to Job
-- (ADR-0028); these are the Job's own overrides, empty when it has none.
ALTER TABLE jobs ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN effort TEXT NOT NULL DEFAULT '';

-- Which phase a Run was carrying out.
ALTER TABLE runs ADD COLUMN phase TEXT NOT NULL DEFAULT '';
