CREATE TABLE attendance_intervals (
 id TEXT PRIMARY KEY, participant_session_id TEXT NOT NULL REFERENCES participant_sessions(id),
 joined_at INTEGER NOT NULL, left_at INTEGER
);
CREATE UNIQUE INDEX attendance_one_open ON attendance_intervals(participant_session_id) WHERE left_at IS NULL;
CREATE TABLE chat_messages (
 id INTEGER PRIMARY KEY AUTOINCREMENT, meeting_id TEXT NOT NULL REFERENCES meetings(id),
 sender_user_id TEXT NOT NULL REFERENCES users(id), sender_name_snapshot TEXT NOT NULL,
 client_message_id TEXT NOT NULL, content TEXT NOT NULL CHECK(length(content) BETWEEN 1 AND 2000),
 created_at INTEGER NOT NULL, UNIQUE(meeting_id,sender_user_id,client_message_id)
);
CREATE INDEX chat_room_cursor ON chat_messages(meeting_id,id);
