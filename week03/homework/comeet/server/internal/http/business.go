package httpapi

import (
	"comeet/server/internal/apperr"
	"comeet/server/internal/auth"
	"comeet/server/internal/config"
	"comeet/server/internal/meeting"
	"comeet/server/internal/realtime"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type identityKey struct{}

func identity(r *http.Request) auth.Identity { return r.Context().Value(identityKey{}).(auth.Identity) }
func fail(w http.ResponseWriter, r *http.Request, err error) {
	var e *apperr.Error
	if errors.As(err, &e) {
		send(w, r, e.Status, e.Code, e.Message, nil)
		return
	}
	send(w, r, 500, "INTERNAL_ERROR", "服务暂时不可用，请稍后重试", nil)
}
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(dst); e != nil {
		fail(w, r, apperr.Invalid("请求 JSON 无效、字段不支持或请求体过大"))
		return false
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		fail(w, r, apperr.Invalid("请求只能包含一个 JSON 对象"))
		return false
	}
	return true
}

// Bounded process-local protection; RemoteAddr is used, never untrusted X-Forwarded-For.
func authLimit() func(http.Handler) http.Handler {
	type entry struct {
		count int
		until time.Time
	}
	var mu sync.Mutex
	entries := map[string]entry{}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, e := net.SplitHostPort(r.RemoteAddr)
			if e != nil {
				host = r.RemoteAddr
			}
			now := time.Now()
			mu.Lock()
			v := entries[host]
			if now.After(v.until) {
				v = entry{0, now.Add(time.Minute)}
			}
			if len(entries) >= 4096 {
				for k, item := range entries {
					if now.After(item.until) {
						delete(entries, k)
					}
				}
			}
			allowed := v.count < 30 && (len(entries) < 4096 || entries[host].count > 0)
			if allowed {
				v.count++
				entries[host] = v
			}
			mu.Unlock()
			if !allowed {
				w.Header().Set("Retry-After", "60")
				send(w, r, 429, "RATE_LIMITED", "操作过于频繁，请稍后再试", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
func business(r chi.Router, db *sql.DB, c config.Config, hub *realtime.Service) {
	a := &auth.Service{DB: db, Config: c}
	m := &meeting.Service{DB: db, Config: c}
	r.Group(func(public chi.Router) {
		public.Use(authLimit())
		public.Post("/api/v1/auth/register", func(w http.ResponseWriter, r *http.Request) {
			var in auth.RegisterInput
			if !decode(w, r, &in) {
				return
			}
			u, e := a.Register(r.Context(), in)
			if e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 201, "OK", "注册成功，请登录", u)
		})
		public.Post("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
			var in auth.LoginInput
			if !decode(w, r, &in) {
				return
			}
			result, e := a.Login(r.Context(), in)
			if e != nil {
				fail(w, r, e)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			send(w, r, 200, "OK", "success", result)
		})
	})
	r.Group(func(private chi.Router) {
		private.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw := r.Header.Get("Authorization")
				if !strings.HasPrefix(raw, "Bearer ") || len(raw) <= 7 {
					send(w, r, 401, "UNAUTHORIZED", "请先登录", nil)
					return
				}
				id, e := a.Authenticate(r.Context(), strings.TrimPrefix(raw, "Bearer "))
				if e != nil {
					fail(w, r, e)
					return
				}
				w.Header().Set("Cache-Control", "no-store")
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
			})
		})
		private.Get("/api/v1/me", func(w http.ResponseWriter, r *http.Request) { send(w, r, 200, "OK", "success", identity(r).User) })
		private.Post("/api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
			if e := a.Logout(r.Context(), identity(r)); e != nil {
				fail(w, r, e)
				return
			}
			if e := hub.RevokeSID(r.Context(), identity(r).SID); e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 200, "OK", "success", nil)
		})
		private.Post("/api/v1/meetings", func(w http.ResponseWriter, r *http.Request) {
			var in meeting.CreateInput
			if !decode(w, r, &in) {
				return
			}
			v, e := m.Create(r.Context(), identity(r), in)
			if e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 201, "OK", "success", v)
		})
		private.Get("/api/v1/meetings", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			limit := 20
			if q.Has("limit") {
				n, e := strconv.Atoi(q.Get("limit"))
				if e != nil {
					fail(w, r, apperr.Invalid("分页参数无效"))
					return
				}
				limit = n
			}
			v, e := m.List(r.Context(), identity(r), q.Get("status"), q.Get("keyword"), q.Get("cursor"), limit)
			if e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 200, "OK", "success", v)
		})
		private.With(authLimit()).Post("/api/v1/meetings/resolve", func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				MeetingNo string `json:"meetingNo"`
			}
			if !decode(w, r, &in) {
				return
			}
			v, e := m.Resolve(r.Context(), in.MeetingNo)
			if e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 200, "OK", "success", v)
		})
		private.Get("/api/v1/meetings/{id}/prepare", func(w http.ResponseWriter, r *http.Request) {
			v, e := m.Prepare(r.Context(), chi.URLParam(r, "id"))
			if e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 200, "OK", "success", v)
		})
		private.Get("/api/v1/meetings/{id}", func(w http.ResponseWriter, r *http.Request) {
			v, e := m.Detail(r.Context(), identity(r), chi.URLParam(r, "id"))
			if e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 200, "OK", "success", v)
		})
		private.Get("/api/v1/media/config", func(w http.ResponseWriter, r *http.Request) {
			send(w, r, 200, "OK", "success", map[string]bool{"configured": c.MediaConfigured()})
		})
		private.Get("/api/v1/meetings/{id}/controls", func(w http.ResponseWriter, r *http.Request) {
			result, e := hub.PendingControls(r.Context(), identity(r), chi.URLParam(r, "id"))
			if e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 200, "OK", "success", result)
		})
		for _, action := range []string{"mute", "remove", "end"} {
			path := "/api/v1/meetings/{id}/members/{userId}/" + action
			if action == "end" {
				path = "/api/v1/meetings/{id}/end"
			}
			private.Post(path, func(w http.ResponseWriter, r *http.Request) {
				var in realtime.ControlInput
				if !decode(w, r, &in) {
					return
				}
				result, e := hub.Control(r.Context(), identity(r), chi.URLParam(r, "id"), action, chi.URLParam(r, "userId"), in)
				if e != nil {
					fail(w, r, e)
					return
				}
				send(w, r, 200, "OK", "success", result)
			})
		}
		private.Post("/api/v1/meetings/{id}/media-token", func(w http.ResponseWriter, r *http.Request) {
			var in realtime.SessionInput
			if !decode(w, r, &in) {
				return
			}
			v, e := hub.MediaToken(r.Context(), identity(r), chi.URLParam(r, "id"), in)
			if e != nil {
				fail(w, r, e)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			send(w, r, 200, "OK", "success", v)
		})
		private.Post("/api/v1/meetings/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
			var in realtime.ResumeInput
			if !decode(w, r, &in) {
				return
			}
			v, e := hub.Resume(r.Context(), identity(r), chi.URLParam(r, "id"), in)
			if e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 200, "OK", "success", v)
		})
		private.Post("/api/v1/meetings/{id}/ws-ticket", func(w http.ResponseWriter, r *http.Request) {
			var in realtime.SessionInput
			if !decode(w, r, &in) {
				return
			}
			v, e := hub.Ticket(r.Context(), identity(r), chi.URLParam(r, "id"), in)
			if e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 200, "OK", "success", v)
		})
		private.Post("/api/v1/meetings/{id}/leave", func(w http.ResponseWriter, r *http.Request) {
			var in realtime.SessionInput
			if !decode(w, r, &in) {
				return
			}
			if e := hub.Leave(r.Context(), identity(r), chi.URLParam(r, "id"), in); e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 200, "OK", "success", nil)
		})
		private.Get("/api/v1/meetings/{id}/members", func(w http.ResponseWriter, r *http.Request) {
			v, e := hub.Members(r.Context(), identity(r), chi.URLParam(r, "id"))
			if e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 200, "OK", "success", v)
		})
		private.Get("/api/v1/meetings/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			limit := 50
			var before, after int64
			var err error
			if q.Has("beforeId") {
				before, err = strconv.ParseInt(q.Get("beforeId"), 10, 64)
				if err != nil || before <= 0 {
					fail(w, r, apperr.Invalid("历史游标无效"))
					return
				}
			}
			if q.Has("afterId") {
				after, err = strconv.ParseInt(q.Get("afterId"), 10, 64)
				if err != nil || after < 0 {
					fail(w, r, apperr.Invalid("补拉游标无效"))
					return
				}
			}
			if q.Has("beforeId") && q.Has("afterId") {
				fail(w, r, apperr.Invalid("beforeId 与 afterId 不能同时使用"))
				return
			}
			if q.Has("limit") {
				limit, err = strconv.Atoi(q.Get("limit"))
				if err != nil {
					fail(w, r, apperr.Invalid("分页数量无效"))
					return
				}
			}
			v, e := hub.Messages(r.Context(), identity(r), chi.URLParam(r, "id"), before, after, q.Has("afterId"), limit)
			if e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 200, "OK", "success", v)
		})
		private.Post("/api/v1/meetings/{id}/join", func(w http.ResponseWriter, r *http.Request) {
			var in meeting.JoinInput
			if !decode(w, r, &in) {
				return
			}
			v, e := m.Join(r.Context(), identity(r), chi.URLParam(r, "id"), in)
			if e != nil {
				fail(w, r, e)
				return
			}
			send(w, r, 200, "OK", "席位预留成功，等待 WebSocket 正式入会", v)
		})
	})
}
