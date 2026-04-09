CREATE TABLE IF NOT EXISTS steps (
    key         TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT,
    position    INT  NOT NULL
);
