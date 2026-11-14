CREATE TABLE summary_jobs (
 id TEXT PRIMARY KEY,
 meeting_id TEXT NOT NULL REFERENCES meetings(id),
 requested_by TEXT NOT NULL REFERENCES users(id),
 status TEXT NOT NULL CHECK(status IN ('running','succeeded','failed')),
 source_hash TEXT NOT NULL,
 source_snapshot TEXT NOT NULL,
 model TEXT NOT NULL,
 result_json TEXT NOT NULL DEFAULT '',
 error_code TEXT NOT NULL DEFAULT '',
 error_message TEXT NOT NULL DEFAULT '',
 diagnostic_id TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL,
 finished_at INTEGER
);
CREATE UNIQUE INDEX summary_one_running ON summary_jobs(meeting_id) WHERE status='running';
CREATE INDEX summary_meeting_history ON summary_jobs(meeting_id,created_at DESC,id);
-- Remember deduplicated request IDs too, including responses lost in transit.
CREATE TABLE summary_requests (
 request_id TEXT PRIMARY KEY,
 meeting_id TEXT NOT NULL REFERENCES meetings(id),
 job_id TEXT NOT NULL REFERENCES summary_jobs(id)
);
CREATE INDEX summary_requests_job ON summary_requests(job_id);
