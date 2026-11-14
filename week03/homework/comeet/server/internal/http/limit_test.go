package httpapi

import (
	"comeet/server/internal/auth"
	"comeet/server/internal/config"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPILimiterBucketsIdentityAndRetryAfter(t *testing.T) {
	gate := apiLimit(config.Config{APIReads: 2, APIWrites: 1, APICreates: 1})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	call := func(sid, method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r = r.WithContext(context.WithValue(context.WithValue(r.Context(), key{}, &requestState{id: "test"}), identityKey{}, auth.Identity{SID: sid}))
		w := httptest.NewRecorder()
		gate.ServeHTTP(w, r)
		return w
	}
	if call("a", "GET", "/api/v1/me").Code != 204 || call("a", "GET", "/api/v1/meetings").Code != 204 {
		t.Fatal("initial reads")
	}
	r := call("a", "GET", "/api/v1/meetings/arbitrary-new-path")
	if r.Code != 429 || r.Header().Get("Retry-After") == "" {
		t.Fatal("missing read limit")
	}
	if call("b", "GET", "/api/v1/me").Code != 204 || call("a", "PUT", "/notes").Code != 204 || call("a", "POST", "/api/v1/meetings").Code != 204 {
		t.Fatal("buckets not separate")
	}
	if call("a", "PUT", "/other-note").Code != 429 || call("a", "POST", "/api/v1/meetings").Code != 429 {
		t.Fatal("writes/create not limited")
	}
}
