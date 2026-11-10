package httpapi

import (
	"bytes"
	"comeet/server/internal/apperr"
	"comeet/server/internal/auth"
	"comeet/server/internal/config"
	"comeet/server/internal/meeting"
	"comeet/server/internal/store"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type harness struct {
	t       *testing.T
	db      *sql.DB
	handler interface {
		ServeHTTP(http.ResponseWriter, *http.Request)
	}
	path string
	c    config.Config
}
type reply struct {
	Code, Message, RequestID string
	Data                     json.RawMessage
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	p := filepath.Join(t.TempDir(), "data", "test.db")
	db, e := store.Open(p)
	if e != nil {
		t.Fatal(e)
	}
	c := config.Config{JWTSecret: "test-only-secret-for-auth-0123456789abcdef", JWTIssuer: "comeet-test", JWTAudience: "comeet-web-test", JWTHours: 8, MaxParticipants: 6}
	h := &harness{t, db, Router(db, c), p, c}
	t.Cleanup(func() { h.db.Close() })
	return h
}
func (h *harness) call(method, path, token string, body any) (int, reply) {
	h.t.Helper()
	var b []byte
	var e error
	if body != nil {
		b, e = json.Marshal(body)
		if e != nil {
			h.t.Fatal(e)
		}
	}
	r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(b))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	var out reply
	if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		h.t.Fatalf("non JSON response %s %d", path, w.Code)
	}
	if len(out.RequestID) != 36 {
		h.t.Fatal("missing request ID")
	}
	return w.Code, out
}
func unpack[T any](t *testing.T, r reply) T {
	t.Helper()
	var v T
	if e := json.Unmarshal(r.Data, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func check(t *testing.T, status int, r reply, wantStatus int, wantCode string) {
	t.Helper()
	if status != wantStatus || r.Code != wantCode {
		t.Fatalf("got %d %s (%s), want %d %s", status, r.Code, r.Message, wantStatus, wantCode)
	}
	if wantStatus >= 400 && string(r.Data) != "null" {
		t.Fatal("error data must be null")
	}
}
func (h *harness) user(name string) auth.LoginResult {
	h.t.Helper()
	s, r := h.call("POST", "/auth/register", "", map[string]string{"username": name, "displayName": name, "password": "Test-password-2026"})
	check(h.t, s, r, 201, "OK")
	s, r = h.call("POST", "/auth/login", "", map[string]string{"username": name, "password": "Test-password-2026"})
	check(h.t, s, r, 200, "OK")
	return unpack[auth.LoginResult](h.t, r)
}
func (h *harness) create(token, title string) meeting.Meeting {
	h.t.Helper()
	s, r := h.call("POST", "/meetings", token, meeting.CreateInput{Title: title, ClientRequestID: uuid.NewString()})
	check(h.t, s, r, 201, "OK")
	return unpack[meeting.Meeting](h.t, r)
}
func TestAccountsValidationAndHash(t *testing.T) {
	h := newHarness(t)
	a := h.user("Alice_01")
	s, r := h.call("POST", "/auth/register", "", map[string]string{"username": "alice_01", "displayName": "另一个", "password": "Test-password-2026"})
	check(t, s, r, 409, "USERNAME_TAKEN")
	for _, in := range []auth.RegisterInput{{Username: "bad name", DisplayName: "姓名", Password: "12345678"}, {Username: "valid_user", DisplayName: "", Password: "12345678"}, {Username: "valid_user", DisplayName: strings.Repeat("中", 33), Password: "12345678"}, {Username: "valid_user", DisplayName: "姓名", Password: "1234567"}, {Username: "valid_user", DisplayName: "姓名", Password: strings.Repeat("中", 25)}} {
		s, r = h.call("POST", "/auth/register", "", in)
		check(t, s, r, 400, "VALIDATION_ERROR")
	}
	var hash string
	if e := h.db.QueryRow("SELECT password_hash FROM users WHERE id=?", a.User.ID).Scan(&hash); e != nil {
		t.Fatal(e)
	}
	if hash == "Test-password-2026" || bcrypt.CompareHashAndPassword([]byte(hash), []byte("Test-password-2026")) != nil {
		t.Fatal("password must be bcrypt hashed")
	}
	var message string
	for _, name := range []string{"Alice_01", "absent_user"} {
		s, r = h.call("POST", "/auth/login", "", auth.LoginInput{Username: name, Password: "wrong-password"})
		check(t, s, r, 401, "UNAUTHORIZED")
		if message != "" && message != r.Message {
			t.Fatal("login leaks account existence")
		}
		message = r.Message
	}
	s, r = h.call("POST", "/auth/login", "", auth.LoginInput{Username: "ALICE_01", Password: "Test-password-2026"})
	check(t, s, r, 200, "OK")
	login := unpack[auth.LoginResult](t, r)
	if login.User.ID != a.User.ID || login.ExpiresAt <= time.Now().UnixMilli() {
		t.Fatal("case normalization or expiry")
	}
	s, r = h.call("GET", "/me", login.Token, nil)
	check(t, s, r, 200, "OK")
	if unpack[auth.User](t, r).ID != a.User.ID {
		t.Fatal("wrong identity")
	}
}
func TestJWTValidationAndRevocation(t *testing.T) {
	h := newHarness(t)
	a := h.user("JWT_User")
	claims := &auth.Claims{}
	_, e := jwt.ParseWithClaims(a.Token, claims, func(*jwt.Token) (any, error) { return []byte(h.c.JWTSecret), nil })
	if e != nil || claims.SID == "" || claims.Subject != a.User.ID || claims.Issuer != h.c.JWTIssuer || claims.IssuedAt == nil || claims.ExpiresAt == nil || len(claims.Audience) != 1 {
		t.Fatal("JWT claims", e)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*auth.Claims)
		method jwt.SigningMethod
		key    string
	}{
		{"wrong signature", func(*auth.Claims) {}, jwt.SigningMethodHS256, "wrong-key"},
		{"wrong algorithm", func(*auth.Claims) {}, jwt.SigningMethodHS384, h.c.JWTSecret},
		{"wrong issuer", func(c *auth.Claims) { c.Issuer = "other" }, jwt.SigningMethodHS256, h.c.JWTSecret},
		{"wrong audience", func(c *auth.Claims) { c.Audience = jwt.ClaimStrings{"other"} }, jwt.SigningMethodHS256, h.c.JWTSecret},
		{"expired", func(c *auth.Claims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute)) }, jwt.SigningMethodHS256, h.c.JWTSecret},
		{"missing expiry", func(c *auth.Claims) { c.ExpiresAt = nil }, jwt.SigningMethodHS256, h.c.JWTSecret},
		{"future issue", func(c *auth.Claims) { c.IssuedAt = jwt.NewNumericDate(time.Now().Add(time.Hour)) }, jwt.SigningMethodHS256, h.c.JWTSecret},
		{"missing sid", func(c *auth.Claims) { c.SID = "" }, jwt.SigningMethodHS256, h.c.JWTSecret},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := *claims
			tc.mutate(&copy)
			bad, e := jwt.NewWithClaims(tc.method, copy).SignedString([]byte(tc.key))
			if e != nil {
				t.Fatal(e)
			}
			s, _ := h.call("GET", "/me", bad, nil)
			if s != 401 {
				t.Fatal("invalid token accepted", s)
			}
		})
	}
	s, r := h.call("GET", "/meetings", "", nil)
	check(t, s, r, 401, "UNAUTHORIZED")
	s, r = h.call("POST", "/auth/logout", a.Token, nil)
	check(t, s, r, 200, "OK")
	s, r = h.call("GET", "/me", a.Token, nil)
	check(t, s, r, 401, "SESSION_EXPIRED")
	s, r = h.call("POST", "/meetings", a.Token, meeting.CreateInput{ClientRequestID: uuid.NewString()})
	check(t, s, r, 401, "SESSION_EXPIRED")
}
func TestMeetingPermissionsAndReservationRetry(t *testing.T) {
	h := newHarness(t)
	a, b := h.user("Host_01"), h.user("Guest_01")
	m := h.create(a.Token, "接口讨论")
	if len(m.MeetingNo) != 9 || strings.IndexFunc(m.MeetingNo, func(r rune) bool { return r < '0' || r > '9' }) >= 0 || m.HostUserID != a.User.ID {
		t.Fatal("meeting identity/number")
	}
	s, r := h.call("GET", "/meetings/"+m.ID, a.Token, nil)
	check(t, s, r, 200, "OK")
	s, r = h.call("GET", "/meetings/"+m.ID, b.Token, nil)
	check(t, s, r, 403, "FORBIDDEN")
	s, r = h.call("POST", "/meetings/resolve", b.Token, map[string]string{"meetingNo": m.MeetingNo[:3] + "-" + m.MeetingNo[3:6] + " " + m.MeetingNo[6:]})
	check(t, s, r, 200, "OK")
	var minimal map[string]any
	json.Unmarshal(r.Data, &minimal)
	if len(minimal) != 5 || minimal["hostUserId"] != nil || minimal["messages"] != nil {
		t.Fatal("prepare must expose minimal fields", minimal)
	}
	s, r = h.call("GET", "/meetings/"+m.ID+"/prepare", b.Token, nil)
	check(t, s, r, 200, "OK")
	req := meeting.JoinInput{ClientRequestID: uuid.NewString()}
	s, r = h.call("POST", "/meetings/"+m.ID+"/join", b.Token, req)
	check(t, s, r, 200, "OK")
	first := unpack[meeting.Reservation](t, r)
	s, r = h.call("POST", "/meetings/"+m.ID+"/join", b.Token, req)
	check(t, s, r, 200, "OK")
	again := unpack[meeting.Reservation](t, r)
	if first != again {
		t.Fatal("retry changed reservation/credential/expiry")
	}
	if first.ExpiresAt < time.Now().Add(55*time.Second).UnixMilli() {
		t.Fatal("invalid reservation ttl")
	}
	var stored []byte
	if e := h.db.QueryRow("SELECT resume_hash FROM participant_sessions WHERE id=?", first.SessionID).Scan(&stored); e != nil {
		t.Fatal(e)
	}
	expected := sha256.Sum256([]byte(first.ResumeCredential))
	if !bytes.Equal(stored, expected[:]) || bytes.Contains(stored, []byte(first.ResumeCredential)) {
		t.Fatal("raw resume credential stored")
	}
	s, r = h.call("POST", "/meetings/"+m.ID+"/join", b.Token, meeting.JoinInput{ClientRequestID: uuid.NewString()})
	check(t, s, r, 409, "ALREADY_JOINED")
	s, r = h.call("GET", "/meetings/"+m.ID, b.Token, nil)
	check(t, s, r, 403, "FORBIDDEN")
	s, r = h.call("GET", "/meetings", b.Token, nil)
	check(t, s, r, 200, "OK")
	if len(unpack[meeting.ListResult](t, r).Items) != 0 {
		t.Fatal("reservation must not create history")
	}
	var members int
	h.db.QueryRow("SELECT COUNT(*) FROM meeting_members").Scan(&members)
	if members != 0 {
		t.Fatal("prepare/reserve wrote attendance")
	}
	s, r = h.call("POST", "/auth/login", "", auth.LoginInput{Username: "Guest_01", Password: "Test-password-2026"})
	check(t, s, r, 200, "OK")
	other := unpack[auth.LoginResult](t, r)
	s, r = h.call("POST", "/meetings/"+m.ID+"/join", other.Token, req)
	check(t, s, r, 409, "ALREADY_JOINED")
	s, r = h.call("POST", "/auth/logout", b.Token, nil)
	check(t, s, r, 200, "OK")
	s, r = h.call("POST", "/meetings/"+m.ID+"/join", other.Token, meeting.JoinInput{ClientRequestID: uuid.NewString()})
	check(t, s, r, 200, "OK")
}
func TestCreateIdempotencyAndPagination(t *testing.T) {
	h := newHarness(t)
	a := h.user("Creator_01")
	input := meeting.CreateInput{Title: "Alpha", ClientRequestID: uuid.NewString()}
	ids := make(chan string, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, r := h.call("POST", "/meetings", a.Token, input)
			if s != 201 {
				t.Errorf("concurrent create %d %s", s, r.Code)
				return
			}
			ids <- unpack[meeting.Meeting](t, r).ID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for v := range ids {
		if id != "" && id != v {
			t.Fatal("idempotency failed")
		}
		id = v
	}
	var n int
	h.db.QueryRow("SELECT COUNT(*) FROM meetings").Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
	input.Title = "different"
	s, r := h.call("POST", "/meetings", a.Token, input)
	check(t, s, r, 409, "IDEMPOTENCY_CONFLICT")
	h.create(a.Token, "Beta")
	s, r = h.call("GET", "/meetings?limit=1", a.Token, nil)
	check(t, s, r, 200, "OK")
	page := unpack[meeting.ListResult](t, r)
	if len(page.Items) != 1 || !page.HasMore || page.NextCursor == "" {
		t.Fatal("pagination")
	}
	s, r = h.call("GET", "/meetings?limit=1&cursor="+page.NextCursor, a.Token, nil)
	check(t, s, r, 200, "OK")
	page2 := unpack[meeting.ListResult](t, r)
	if len(page2.Items) != 1 || page2.HasMore || page2.Items[0].ID == page.Items[0].ID {
		t.Fatal("pagination duplicates")
	}
	s, r = h.call("GET", "/meetings?keyword=Alpha", a.Token, nil)
	check(t, s, r, 200, "OK")
	if len(unpack[meeting.ListResult](t, r).Items) != 1 {
		t.Fatal("keyword filter")
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?cursor=bad", "?status=unknown"} {
		s, r = h.call("GET", "/meetings"+query, a.Token, nil)
		check(t, s, r, 400, "VALIDATION_ERROR")
	}
	s, r = h.call("POST", "/meetings", a.Token, map[string]any{"title": "spoof", "hostUserId": uuid.NewString(), "clientRequestId": uuid.NewString()})
	check(t, s, r, 400, "VALIDATION_ERROR")
}
func TestCapacityConcurrencyAndExpiry(t *testing.T) {
	h := newHarness(t)
	host := h.user("CapacityHost")
	m := h.create(host.Token, "capacity")
	users := make([]auth.LoginResult, 8)
	for i := range users {
		users[i] = h.user(fmt.Sprintf("Capacity_%d", i))
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, full := 0, 0
	var successful []meeting.Reservation
	for _, u := range users {
		wg.Add(1)
		go func(u auth.LoginResult) {
			defer wg.Done()
			s, r := h.call("POST", "/meetings/"+m.ID+"/join", u.Token, meeting.JoinInput{ClientRequestID: uuid.NewString()})
			mu.Lock()
			defer mu.Unlock()
			if s == 200 {
				ok++
				successful = append(successful, unpack[meeting.Reservation](t, r))
			} else if s == 409 && r.Code == "MEETING_FULL" {
				full++
			} else {
				t.Errorf("unexpected %d %s", s, r.Code)
			}
		}(u)
	}
	wg.Wait()
	if ok != 6 || full != 2 {
		t.Fatalf("capacity ok=%d full=%d", ok, full)
	}
	h.db.Exec("UPDATE participant_sessions SET reserved_until=? WHERE id=?", time.Now().Add(-time.Second).UnixMilli(), successful[0].SessionID)
	s, r := h.call("POST", "/meetings/"+m.ID+"/join", host.Token, meeting.JoinInput{ClientRequestID: uuid.NewString()})
	check(t, s, r, 200, "OK")
	var n int
	h.db.QueryRow("SELECT COUNT(*) FROM participant_sessions WHERE meeting_id=? AND state='joining'", m.ID).Scan(&n)
	if n != 6 {
		t.Fatal("expired slot not released", n)
	}
	// The database independently enforces the per-user active-session invariant.
	_, e := h.db.Exec(`INSERT INTO participant_sessions(id,meeting_id,user_id,auth_session_id,resume_hash,state,reserved_until,client_request_id) SELECT ?,meeting_id,user_id,auth_session_id,resume_hash,state,reserved_until,? FROM participant_sessions WHERE id=?`, uuid.NewString(), uuid.NewString(), unpack[meeting.Reservation](t, r).SessionID)
	if !apperr.IsUnique(e) {
		t.Fatal("partial unique index missing or wrong failure", e)
	}
}
func TestStatusesHistoryPermissionsAndPersistence(t *testing.T) {
	h := newHarness(t)
	a, b := h.user("PersistHost"), h.user("PersistGuest")
	m := h.create(a.Token, "persist")
	for _, status := range []string{"ending", "ended"} {
		h.db.Exec("UPDATE meetings SET status=? WHERE id=?", status, m.ID)
		code := "MEETING_ENDING"
		if status == "ended" {
			code = "MEETING_ENDED"
		}
		s, r := h.call("GET", "/meetings/"+m.ID+"/prepare", b.Token, nil)
		check(t, s, r, 409, code)
		s, r = h.call("POST", "/meetings/"+m.ID+"/join", b.Token, meeting.JoinInput{ClientRequestID: uuid.NewString()})
		check(t, s, r, 409, code)
	}
	s, r := h.call("GET", "/meetings/"+m.ID, b.Token, nil)
	check(t, s, r, 403, "FORBIDDEN")
	// Fixtures represent actual attendance from a future stage, never runtime seed data.
	h.db.Exec("INSERT INTO meeting_members(id,meeting_id,user_id,display_name_snapshot,first_joined_at) VALUES (?,?,?,?,?)", uuid.NewString(), m.ID, b.User.ID, b.User.DisplayName, time.Now().UnixMilli())
	s, r = h.call("GET", "/meetings/"+m.ID, b.Token, nil)
	check(t, s, r, 200, "OK")
	s, r = h.call("GET", "/meetings?status=ended", b.Token, nil)
	check(t, s, r, 200, "OK")
	if len(unpack[meeting.ListResult](t, r).Items) != 1 {
		t.Fatal("actual participant cannot see history")
	}
	h.db.Close()
	db, e := store.Open(h.path)
	if e != nil {
		t.Fatal(e)
	}
	h.db = db
	h.handler = Router(db, h.c)
	s, r = h.call("GET", "/me", a.Token, nil)
	check(t, s, r, 200, "OK")
	s, r = h.call("GET", "/meetings/"+m.ID, a.Token, nil)
	check(t, s, r, 200, "OK")
	if unpack[meeting.Meeting](t, r).Title != "persist" {
		t.Fatal("database lost after restart")
	}
}
func TestInvalidBodiesUnknownMeetingsAndRateLimit(t *testing.T) {
	h := newHarness(t)
	a := h.user("ValidationUser")
	for _, no := range []string{"12", "abc-123-456", "１２３４５６７８９"} {
		s, r := h.call("POST", "/meetings/resolve", a.Token, map[string]string{"meetingNo": no})
		check(t, s, r, 400, "VALIDATION_ERROR")
	}
	s, r := h.call("POST", "/meetings/resolve", a.Token, map[string]string{"meetingNo": "000000000"})
	check(t, s, r, 404, "MEETING_NOT_FOUND")
	s, r = h.call("GET", "/meetings/"+uuid.NewString(), a.Token, nil)
	check(t, s, r, 404, "MEETING_NOT_FOUND")
	for _, body := range []string{`{`, `{} {}`, strings.Repeat("x", (1<<20)+1)} {
		req := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(body))
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatal("bad body accepted", w.Code)
		}
	}
	hit := false
	for i := 0; i < 35; i++ {
		s, r = h.call("POST", "/auth/login", "", auth.LoginInput{Username: "missing", Password: "x"})
		if s == 429 {
			check(t, s, r, 429, "RATE_LIMITED")
			hit = true
			break
		}
	}
	if !hit {
		t.Fatal("rate limiter inactive")
	}
	hit = false
	for i := 0; i < 35; i++ {
		status, r := h.call("POST", "/meetings/resolve", a.Token, map[string]string{"meetingNo": "bad"})
		if status == 429 {
			check(t, status, r, 429, "RATE_LIMITED")
			hit = true
			break
		}
	}
	if !hit {
		t.Fatal("resolve limiter inactive")
	}
}

func TestConcurrentSameUserAndExpiredReservation(t *testing.T) {
	h := newHarness(t)
	a := h.user("ConcurrentUser")
	m := h.create(a.Token, "one slot")
	type result struct {
		status int
		r      reply
		req    string
	}
	results := make(chan result, 6)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := uuid.NewString()
			s, r := h.call("POST", "/meetings/"+m.ID+"/join", a.Token, meeting.JoinInput{ClientRequestID: id})
			results <- result{s, r, id}
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	var reservation meeting.Reservation
	var original string
	for v := range results {
		if v.status == 200 {
			success++
			reservation = unpack[meeting.Reservation](t, v.r)
			original = v.req
		} else {
			check(t, v.status, v.r, 409, "ALREADY_JOINED")
		}
	}
	if success != 1 {
		t.Fatal("same user reserved multiple seats", success)
	}
	if _, e := h.db.Exec("UPDATE participant_sessions SET reserved_until=? WHERE id=?", time.Now().Add(-time.Second).UnixMilli(), reservation.SessionID); e != nil {
		t.Fatal(e)
	}
	s, r := h.call("POST", "/meetings/"+m.ID+"/join", a.Token, meeting.JoinInput{ClientRequestID: original})
	check(t, s, r, 409, "SESSION_EXPIRED")
	s, r = h.call("POST", "/meetings/"+m.ID+"/join", a.Token, meeting.JoinInput{ClientRequestID: uuid.NewString()})
	check(t, s, r, 200, "OK")
	if unpack[meeting.Reservation](t, r).SessionID == reservation.SessionID {
		t.Fatal("expired request revived")
	}
}
func TestReservationRestartAndBan(t *testing.T) {
	h := newHarness(t)
	a := h.user("RestartUser")
	m := h.create(a.Token, "restart reservation")
	input := meeting.JoinInput{ClientRequestID: uuid.NewString()}
	s, r := h.call("POST", "/meetings/"+m.ID+"/join", a.Token, input)
	check(t, s, r, 200, "OK")
	first := unpack[meeting.Reservation](t, r)
	h.db.Close()
	db, e := store.Open(h.path)
	if e != nil {
		t.Fatal(e)
	}
	h.db = db
	h.handler = Router(db, h.c)
	s, r = h.call("POST", "/meetings/"+m.ID+"/join", a.Token, input)
	check(t, s, r, 200, "OK")
	if unpack[meeting.Reservation](t, r) != first {
		t.Fatal("retry after restart changed reservation")
	}
	if _, e = h.db.Exec("INSERT INTO meeting_bans VALUES (?,?,?,?)", m.ID, a.User.ID, a.User.ID, time.Now().UnixMilli()); e != nil {
		t.Fatal(e)
	}
	s, r = h.call("POST", "/meetings/"+m.ID+"/join", a.Token, input)
	check(t, s, r, 403, "REMOVED_FROM_MEETING")
}

func TestExpiredAuthDoesNotKeepReservedSeat(t *testing.T) {
	h := newHarness(t)
	h.c.MaxParticipants = 1
	h.handler = Router(h.db, h.c)
	a, b := h.user("ExpiringHost"), h.user("NextGuest")
	m := h.create(a.Token, "expiry slot")
	s, r := h.call("POST", "/meetings/"+m.ID+"/join", a.Token, meeting.JoinInput{ClientRequestID: uuid.NewString()})
	check(t, s, r, 200, "OK")
	if _, e := h.db.Exec("UPDATE auth_sessions SET expires_at=? WHERE user_id=?", time.Now().Add(-time.Second).UnixMilli(), a.User.ID); e != nil {
		t.Fatal(e)
	}
	s, r = h.call("GET", "/me", a.Token, nil)
	check(t, s, r, 401, "SESSION_EXPIRED")
	s, r = h.call("POST", "/meetings/"+m.ID+"/join", b.Token, meeting.JoinInput{ClientRequestID: uuid.NewString()})
	check(t, s, r, 200, "OK")
}
