-- Accounts are the subscriptions Owl runs work on (ADR-0019). Each has a
-- configuration directory of its own, so that Owl's nights never disturb the
-- user's own tool setup, and a reference to where its secret is kept: the
-- secret itself belongs in the OS keychain, never here.
CREATE TABLE accounts (
    name             TEXT PRIMARY KEY,
    driver           TEXT NOT NULL,
    config_dir       TEXT NOT NULL,
    credential_ref   TEXT NOT NULL,
    failover_allowed INTEGER NOT NULL DEFAULT 0,
    created          TEXT NOT NULL
) STRICT;

-- Which Account a Job ran on. It is taken from the Project's configuration at
-- the Job's first Run and does not change afterwards, so what a Job drew on
-- stays knowable for its whole life (ADR-0023). Empty until it has run.
--
-- There is deliberately no foreign key: a Job names the Account it ran on even
-- after that Account is gone, and removing one that Jobs still reference is
-- refused with a message rather than by a constraint nobody can read.
ALTER TABLE jobs ADD COLUMN account TEXT NOT NULL DEFAULT '';
