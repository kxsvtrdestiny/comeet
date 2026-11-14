package store

import (
	"comeet/server/migrations"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrationPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "test.db")
	for i := 0; i < 2; i++ {
		db, e := Open(path)
		if e != nil {
			t.Fatal(e)
		}
		var count int
		if e = db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); e != nil || count != 7 {
			t.Fatalf("migration count=%d error=%v", count, e)
		}
		var foreignKeys, timeout int
		var mode string
		db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys)
		db.QueryRow("PRAGMA busy_timeout").Scan(&timeout)
		db.QueryRow("PRAGMA journal_mode").Scan(&mode)
		if foreignKeys != 1 || timeout != 5000 || mode != "wal" {
			t.Fatalf("pragmas: %d %d %s", foreignKeys, timeout, mode)
		}
		if i == 0 {
			if _, e = db.Exec("INSERT INTO app_metadata VALUES ('persistence_test','retained')"); e != nil {
				t.Fatal(e)
			}
		} else {
			var v string
			if e = db.QueryRow("SELECT value FROM app_metadata WHERE key='persistence_test'").Scan(&v); e != nil || v != "retained" {
				t.Fatal("data not preserved", e)
			}
		}
		db.Close()
	}
}

func TestUpgradeStage5DatabasePreservesExistingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	db.Exec("CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at INTEGER NOT NULL)")
	for _, name := range []string{"001_framework.sql", "002_accounts_meetings.sql", "003_realtime_chat.sql", "004_sharing_moderation.sql"} {
		body, e := migrations.Files.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(string(body)); e != nil {
			t.Fatal(e)
		}
		db.Exec("INSERT INTO schema_migrations VALUES (?,1)", name)
	}
	for _, q := range []string{
		"INSERT INTO users VALUES ('old-user','old_user','old_user','旧账号','preserved-hash',1)",
		"INSERT INTO meetings(id,meeting_no,title,host_user_id,livekit_room_name,status,started_at,client_request_id) VALUES ('old-meeting','123456789','旧会议','old-user','old-meeting','ended',1,'old-request')",
		"INSERT INTO chat_messages(meeting_id,sender_user_id,sender_name_snapshot,client_message_id,content,created_at) VALUES ('old-meeting','old-user','旧姓名','old-message','旧聊天必须保留',2)",
	} {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	db.Close()
	for i := 0; i < 2; i++ {
		db, e = Open(path)
		if e != nil {
			t.Fatal(e)
		}
		var content, hash string
		db.QueryRow("SELECT content FROM chat_messages").Scan(&content)
		db.QueryRow("SELECT password_hash FROM users").Scan(&hash)
		if content != "旧聊天必须保留" || hash != "preserved-hash" {
			t.Fatal("upgrade changed old data")
		}
		var n int
		db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n)
		if n != 7 {
			t.Fatal(n)
		}
		db.Close()
	}
}

func TestStage6UpgradeKeepsNotesAndModerationCheckpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stage6.db")
	db, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{
		"DROP TABLE summary_requests", "DROP TABLE summary_jobs", "DELETE FROM schema_migrations WHERE version='007_deepseek_summaries.sql'",
		"INSERT INTO users VALUES ('legacy','legacy','legacy','旧账号','retained',1)",
		"INSERT INTO meetings(id,meeting_no,title,host_user_id,livekit_room_name,status,started_at,client_request_id) VALUES ('legacy-meeting','123456789','旧会议','legacy','legacy-room','ending',1,'legacy-request')",
		"INSERT INTO meeting_notes VALUES ('legacy-note','legacy-meeting','legacy','第六阶段笔记',3,1)",
		"INSERT INTO moderation_operations(id,meeting_id,actor_id,action,target_user_id,state,created_at) VALUES ('legacy-op','legacy-meeting','legacy','end','','pending',1)",
		"INSERT INTO moderation_cleanup_steps VALUES ('legacy-op','remove','old-session',1)",
	} {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	db.Close()
	for i := 0; i < 2; i++ {
		db, e = Open(path)
		if e != nil {
			t.Fatal(e)
		}
		var content, status string
		var version, done, count int
		db.QueryRow("SELECT content,version FROM meeting_notes WHERE id='legacy-note'").Scan(&content, &version)
		db.QueryRow("SELECT status FROM meetings WHERE id='legacy-meeting'").Scan(&status)
		db.QueryRow("SELECT completed_at FROM moderation_cleanup_steps WHERE operation_id='legacy-op'").Scan(&done)
		db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
		if content != "第六阶段笔记" || version != 3 || status != "ending" || done != 1 || count != 7 {
			t.Fatal("migration lost stage6 state", content, version, status, done, count)
		}
		db.Close()
	}
}
