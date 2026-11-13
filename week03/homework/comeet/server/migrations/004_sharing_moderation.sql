-- Only the grants actually issued by this server require external cleanup.
CREATE TABLE media_grants (
 session_id TEXT PRIMARY KEY REFERENCES participant_sessions(id),
 meeting_id TEXT NOT NULL REFERENCES meetings(id), expires_at INTEGER NOT NULL
);
CREATE INDEX media_grants_meeting ON media_grants(meeting_id);
-- Stage-4 tokens were not recorded; conservatively retain active identities on upgrade.
INSERT INTO media_grants(session_id,meeting_id,expires_at)
 SELECT id,meeting_id,CAST(strftime('%s','now') AS INTEGER)*1000+300000 FROM participant_sessions WHERE joined_at IS NOT NULL AND (state IN ('online','reconnecting') OR left_at>=CAST(strftime('%s','now') AS INTEGER)*1000-300000);
CREATE TABLE moderation_operations (
 id TEXT PRIMARY KEY, meeting_id TEXT NOT NULL REFERENCES meetings(id),
 actor_id TEXT NOT NULL REFERENCES users(id), action TEXT NOT NULL CHECK(action IN ('mute','remove','end')),
 target_user_id TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('pending','completed')), created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX moderation_final_action ON moderation_operations(meeting_id,action,target_user_id) WHERE action IN ('remove','end');
