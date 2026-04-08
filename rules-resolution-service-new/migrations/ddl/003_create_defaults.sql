CREATE TABLE IF NOT EXISTS defaults (
    step_key    TEXT NOT NULL REFERENCES steps(key),
    trait_key   TEXT NOT NULL,
    value       JSONB NOT NULL,
    PRIMARY KEY (step_key, trait_key)
);
