CREATE TABLE users (
 id TEXT PRIMARY KEY, username TEXT NOT NULL, normalized_username TEXT NOT NULL UNIQUE,
 display_name TEXT NOT NULL, password_hash TEXT NOT NULL, created_at INTEGER NOT NULL
);
CREATE TABLE auth_sessions (
 id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), expires_at INTEGER NOT NULL,
 revoked_at INTEGER, created_at INTEGER NOT NULL
);
CREATE INDEX auth_sessions_user ON auth_sessions(user_id);
CREATE TABLE meetings (
 id TEXT PRIMARY KEY, meeting_no TEXT NOT NULL UNIQUE CHECK(length(meeting_no)=9), title TEXT NOT NULL,
 host_user_id TEXT NOT NULL REFERENCES users(id), livekit_room_name TEXT NOT NULL UNIQUE,
 status TEXT NOT NULL CHECK(status IN ('active','ending','ended')), started_at INTEGER NOT NULL,
 ended_at INTEGER, last_empty_at INTEGER, client_request_id TEXT NOT NULL,
 UNIQUE(host_user_id,client_request_id)
);
CREATE TABLE meeting_members (
 id TEXT PRIMARY KEY, meeting_id TEXT NOT NULL REFERENCES meetings(id), user_id TEXT NOT NULL REFERENCES users(id),
 display_name_snapshot TEXT NOT NULL, first_joined_at INTEGER NOT NULL, last_left_at INTEGER,
 UNIQUE(meeting_id,user_id)
);
CREATE TABLE participant_sessions (
 id TEXT PRIMARY KEY, meeting_id TEXT NOT NULL REFERENCES meetings(id), user_id TEXT NOT NULL REFERENCES users(id),
 auth_session_id TEXT NOT NULL REFERENCES auth_sessions(id), resume_hash BLOB NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('joining','online','reconnecting','left')), generation INTEGER NOT NULL DEFAULT 0,
 reserved_until INTEGER NOT NULL, joined_at INTEGER, last_seen_at INTEGER, disconnected_at INTEGER, left_at INTEGER,
 client_request_id TEXT NOT NULL, UNIQUE(meeting_id,user_id,client_request_id)
);
CREATE UNIQUE INDEX participant_one_active ON participant_sessions(meeting_id,user_id)
 WHERE state IN ('joining','online','reconnecting');
CREATE INDEX participant_capacity ON participant_sessions(meeting_id,state);
CREATE TABLE meeting_bans (
 meeting_id TEXT NOT NULL REFERENCES meetings(id), user_id TEXT NOT NULL REFERENCES users(id),
 created_by TEXT NOT NULL REFERENCES users(id), created_at INTEGER NOT NULL,
 PRIMARY KEY(meeting_id,user_id)
);
