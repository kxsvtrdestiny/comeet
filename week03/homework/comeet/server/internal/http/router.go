package httpapi

import (
	"comeet/server/internal/config"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"log/slog"
	"net/http"
	"time"
)

type Response struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data"`
	RequestID string `json:"requestId"`
}
type Health struct {
	Database        string `json:"database"`
	SchemaVersion   string `json:"schemaVersion"`
	MediaConfigured bool   `json:"mediaConfigured"`
	AIConfigured    bool   `json:"aiConfigured"`
	Stage           int    `json:"stage"`
}
type key struct{}
type requestState struct{ id, code string }

func send(w http.ResponseWriter, r *http.Request, status int, code, message string, data any) {
	state := r.Context().Value(key{}).(*requestState)
	state.code = code
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Response{code, message, data, state.id})
}
func Router(db *sql.DB, c config.Config) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b := make([]byte, 16)
			rand.Read(b)
			b[6] = (b[6] & 15) | 64
			b[8] = (b[8] & 63) | 128
			raw := hex.EncodeToString(b)
			id := raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
			w.Header().Set("X-Request-ID", id)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			r = r.WithContext(context.WithValue(r.Context(), key{}, &requestState{id: id, code: "OK"}))
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			start := time.Now()
			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(wrapped, r)
			slog.Info("http_request", "request_id", id, "operation", r.Method+" "+r.URL.Path, "status", wrapped.Status(), "error_code", r.Context().Value(key{}).(*requestState).code, "duration_ms", time.Since(start).Milliseconds())
		})
	})
	r.Get("/api/v1/health/live", func(w http.ResponseWriter, r *http.Request) {
		send(w, r, 200, "OK", "success", map[string]string{"status": "alive"})
	})
	r.Get("/api/v1/health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		var version string
		if e := db.QueryRowContext(ctx, "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&version); e != nil {
			send(w, r, 503, "DATABASE_UNAVAILABLE", "数据库暂不可用", nil)
			return
		}
		send(w, r, 200, "OK", "success", Health{"ready", version, c.MediaConfigured(), c.AIConfigured(), 2})
	})
	business(r, db, c)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		send(w, r, 404, "NOT_FOUND", "接口不存在或尚未开发", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		send(w, r, 405, "METHOD_NOT_ALLOWED", "请求方法不支持", nil)
	})
	return r
}
