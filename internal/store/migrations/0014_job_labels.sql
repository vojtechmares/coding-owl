-- The labels a Job carries (issue #132). A label is a name and nothing else:
-- it says what kind of work the Job is, and carries no meaning for the
-- scheduler, which goes on scanning the queue in order (ADR-0025). There is
-- deliberately no priority hiding in here.
--
-- The labels live in a table of their own rather than packed into a column on
-- jobs, because owl jobs label add and owl jobs label remove change one label
-- at a time and should not have to rewrite the Job's row to do it. The
-- composite primary key is what makes a Job carry a label once however many
-- times it is added.
CREATE TABLE job_labels (
    job_id INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    label  TEXT NOT NULL,
    PRIMARY KEY (job_id, label)
) STRICT;

-- owl queue list --label asks which Jobs carry a label, which is the opposite
-- direction to the primary key.
CREATE INDEX job_labels_label ON job_labels (label);
