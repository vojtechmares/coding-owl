-- The chat the desktop app holds with a model (ADR-0022). The daemon keeps
-- the conversations, so they survive the app; the model credentials are not
-- here, only a reference to where they are kept (ADR-0019).
CREATE TABLE chat_providers (
    name           TEXT PRIMARY KEY COLLATE NOCASE,
    -- base_url is where the provider is reached, empty for its own default.
    base_url       TEXT NOT NULL DEFAULT '',
    -- models are the models this provider offers, one per line.
    models         TEXT NOT NULL DEFAULT '',
    credential_ref TEXT NOT NULL,
    created        TEXT NOT NULL
) STRICT;

CREATE TABLE conversations (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    -- title is what the conversation is about, taken from what was said first.
    title   TEXT NOT NULL DEFAULT '',
    -- model is what was last spoken to, so a conversation reopens where it was.
    model   TEXT NOT NULL DEFAULT '',
    created TEXT NOT NULL,
    updated TEXT NOT NULL,
    -- updated_unix is when it was last spoken to, in nanoseconds, because a
    -- time written as text does not sort as a time and this column is what
    -- orders the list a reader sees.
    updated_unix INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE INDEX conversations_by_spoken_to ON conversations (updated_unix DESC, id DESC);

CREATE TABLE chat_messages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    -- role is who said it: `user` or `assistant`.
    role            TEXT NOT NULL,
    text            TEXT NOT NULL,
    created         TEXT NOT NULL
) STRICT;

CREATE INDEX chat_messages_by_conversation ON chat_messages (conversation_id, id);
