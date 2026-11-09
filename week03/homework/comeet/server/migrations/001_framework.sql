-- Stage 1 only: business tables arrive in their own stages.
CREATE TABLE app_metadata (
 key TEXT PRIMARY KEY,
 value TEXT NOT NULL
);
INSERT INTO app_metadata(key, value) VALUES ('schema_purpose', 'comeet-stage-1');
