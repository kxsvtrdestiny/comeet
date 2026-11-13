package store

import (
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
		if e = db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); e != nil || count != 4 {
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
