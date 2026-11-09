package httpapi

import (
	"comeet/server/internal/config"
	"comeet/server/internal/store"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHealthAndFailure(t *testing.T) {
	db, e := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	h := Router(db, config.Config{})
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/api/v1/health/live", 200}, {"GET", "/api/v1/health/ready", 200}, {"POST", "/api/v1/health/live", 405}, {"GET", "/api/v1/auth/login", 404}} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(tc, w.Code)
		}
		var b Response
		if e = json.Unmarshal(w.Body.Bytes(), &b); e != nil || len(b.RequestID) != 36 {
			t.Fatal("bad envelope", e)
		}
		if tc.status == 200 && tc.path == "/api/v1/health/ready" {
			data := b.Data.(map[string]any)
			if data["database"] != "ready" || data["mediaConfigured"] != false || data["aiConfigured"] != false {
				t.Fatal(data)
			}
		}
	}
	db.Close()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/health/ready", nil))
	if w.Code != 503 {
		t.Fatal("closed db must fail", w.Code)
	}
}
