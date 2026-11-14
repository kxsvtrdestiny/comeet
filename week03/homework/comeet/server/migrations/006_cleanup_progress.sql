-- Durable per-operation checkpoints: successful removals/deletion survive retries/restarts.
CREATE TABLE moderation_cleanup_steps (
 operation_id TEXT NOT NULL REFERENCES moderation_operations(id),
 step TEXT NOT NULL CHECK(step IN ('close_sessions','snapshot','remove','delete_room')),
 identity TEXT NOT NULL DEFAULT '',
 completed_at INTEGER,
 PRIMARY KEY(operation_id,step,identity)
);
ALTER TABLE moderation_operations ADD COLUMN last_error_code TEXT NOT NULL DEFAULT '';
ALTER TABLE moderation_operations ADD COLUMN last_error_step TEXT NOT NULL DEFAULT '';
ALTER TABLE moderation_operations ADD COLUMN last_diagnostic_id TEXT NOT NULL DEFAULT '';
ALTER TABLE moderation_operations ADD COLUMN last_error_message TEXT NOT NULL DEFAULT '';
