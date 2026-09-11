-- Which Verifier said it (ADR-0013). Verification has two: the Project's own
-- checks, and the fresh Agent Session a Project can ask to judge the work.
-- What that Agent says is its answer rather than evidence for a failure, so a
-- report shows it whether it passed or not, and that needs telling apart.
ALTER TABLE check_results ADD COLUMN verifier TEXT NOT NULL DEFAULT 'command';
