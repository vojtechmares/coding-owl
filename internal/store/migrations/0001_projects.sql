-- Projects registered with Coding Owl. The name is the identity (ADR-0031);
-- the path is an ordinary mutable attribute.
CREATE TABLE projects (
    name        TEXT PRIMARY KEY,
    path        TEXT NOT NULL,
    base_branch TEXT NOT NULL,
    registered  TEXT NOT NULL
) STRICT;
