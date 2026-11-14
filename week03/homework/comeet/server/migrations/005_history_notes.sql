CREATE TABLE meeting_notes (
 id TEXT PRIMARY KEY,
 meeting_id TEXT NOT NULL REFERENCES meetings(id),
 author_user_id TEXT NOT NULL REFERENCES users(id),
 content TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version>=1),
 updated_at INTEGER NOT NULL,
 UNIQUE(meeting_id,author_user_id)
);
CREATE INDEX attendance_sessions_joined ON attendance_intervals(participant_session_id,joined_at);
CREATE INDEX meetings_status_started ON meetings(status,started_at);
