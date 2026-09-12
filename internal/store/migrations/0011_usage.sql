-- What a Run's stream last said about an Account's utilization, per window
-- (ADR-0020). Utilization is account-wide: it counts what the user spent
-- themselves as well as what Owl did, which is why a ceiling on it leaves the
-- user headroom without Owl being told what they are doing.
--
-- One row per Account and window, replaced by every Run that reports one.
-- There is deliberately no foreign key: a reading outlives the Account it is
-- about no more usefully than it outlives its own window, and both are cleared
-- rather than constrained.
CREATE TABLE account_usage (
    account      TEXT NOT NULL COLLATE NOCASE,
    window_name  TEXT NOT NULL,
    utilization  REAL NOT NULL,
    -- When the window starts again, and when this was read, as seconds since
    -- the epoch: a reading is compared with now, and text that sorts by
    -- accident is not a time (ADR-0025).
    resets_unix   INTEGER NOT NULL,
    observed_unix INTEGER NOT NULL,
    PRIMARY KEY (account, window_name)
) STRICT;
