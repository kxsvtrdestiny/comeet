package httpapi

import (
	"comeet/server/internal/auth"
	"context"
	"errors"
	"fmt"
	"github.com/twitchtv/twirp"
	"strings"
	"sync"
	"testing"
	"time"

	"comeet/server/internal/meeting"
	"comeet/server/internal/realtime"
	"github.com/google/uuid"
)

type fakeAdmin struct {
	listErr, removeErr, deleteErr    error
	listed                           []string
	latency                          time.Duration
	mu                               sync.Mutex
	failMute, failRemove, failDelete bool
	muted, removed, deleted          int
	cutoff                           int64
	identities                       []string
	entered, release                 chan struct{}
}

func (f *fakeAdmin) List(context.Context, string) ([]string, error) { return f.listed, f.listErr }
func (f *fakeAdmin) Mute(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.muted++
	if f.failMute {
		return errors.New("upstream unavailable")
	}
	return nil
}
func (f *fakeAdmin) Remove(ctx context.Context, room, id string, cutoff int64) error {
	f.mu.Lock()
	f.removed++
	f.identities = append(f.identities, id)
	f.cutoff = cutoff
	fail, entered, release := f.failRemove, f.entered, f.release
	f.mu.Unlock()
	if f.latency > 0 {
		select {
		case <-time.After(f.latency):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.removeErr != nil {
		return f.removeErr
	}
	if fail {
		return errors.New("upstream unavailable")
	}
	return nil
}
func (f *fakeAdmin) Delete(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if f.failDelete {
		return errors.New("delete unavailable")
	}
	return nil
}
func grantFixture(t *testing.T, h *wsHarness, meetingID, sessionID string) {
	t.Helper()
	if _, e := h.db.Exec("INSERT INTO media_grants VALUES (?,?,?)", sessionID, meetingID, time.Now().Add(5*time.Minute).UnixMilli()); e != nil {
		t.Fatal(e)
	}
}
func controlInput() realtime.ControlInput { return realtime.ControlInput{RequestID: uuid.NewString()} }
func TestModerationHostPermissionsAndOneShotMute(t *testing.T) {
	h := newWSHarness(t)
	a, b := h.user("ControlHost"), h.user("ControlGuest")
	m := h.create(a.Token, "权限与一次静音")
	ra, rb := h.reserve(a.Token, m.ID), h.reserve(b.Token, m.ID)
	ca, cb := h.connect(a.Token, m.ID, ra.SessionID), h.connect(b.Token, m.ID, rb.SessionID)
	f := &fakeAdmin{}
	h.hub.MediaAdmin = f
	for _, action := range []string{"mute", "remove", "end"} {
		path := "/meetings/" + m.ID + "/members/" + a.User.ID + "/" + action
		if action == "end" {
			path = "/meetings/" + m.ID + "/end"
		}
		s, r := h.call("POST", path, b.Token, controlInput())
		check(t, s, r, 403, "HOST_REQUIRED")
	}
	path := "/meetings/" + m.ID + "/members/" + b.User.ID + "/mute"
	s, r := h.call("POST", path, a.Token, controlInput())
	check(t, s, r, 409, "MICROPHONE_NOT_PUBLISHED")
	grantFixture(t, h, m.ID, rb.SessionID)
	op := controlInput()
	f.failMute = true
	s, r = h.call("POST", path, a.Token, op)
	check(t, s, r, 502, "MEDIA_CONTROL_FAILED")
	f.failMute = false
	s, r = h.call("POST", path, a.Token, op)
	check(t, s, r, 200, "OK")
	cb.wait("member.muted")
	s, r = h.call("POST", path, a.Token, op)
	check(t, s, r, 200, "OK")
	if f.muted != 2 {
		t.Fatal("duplicate completed mute was replayed", f.muted)
	}
	s, r = h.call("POST", path, a.Token, controlInput())
	check(t, s, r, 200, "OK")
	ca.send("ping", map[string]any{})
	ca.wait("pong")
	// The target's publishing permission and membership are unchanged.
	var n int
	h.db.QueryRow("SELECT COUNT(*) FROM meeting_bans WHERE meeting_id=?", m.ID).Scan(&n)
	if n != 0 {
		t.Fatal("mute created a ban")
	}
	s, r = h.call("POST", "/meetings/"+m.ID+"/members/"+a.User.ID+"/remove", a.Token, controlInput())
	check(t, s, r, 400, "VALIDATION_ERROR")
	s, r = h.call("POST", path, a.Token, map[string]any{"requestId": uuid.NewString(), "muted": false})
	check(t, s, r, 400, "VALIDATION_ERROR")
	s, r = h.call("GET", "/meetings/"+m.ID+"/controls", b.Token, nil)
	check(t, s, r, 403, "HOST_REQUIRED")
}
func TestRemoveFailureBansAllReentryAndRetryDoesNotBlockChat(t *testing.T) {
	h := newWSHarness(t)
	a, b := h.user("RemoveHost"), h.user("RemoveGuest")
	m := h.create(a.Token, "移出失败与重试")
	ra, rb := h.reserve(a.Token, m.ID), h.reserve(b.Token, m.ID)
	ca, cb := h.connect(a.Token, m.ID, ra.SessionID), h.connect(b.Token, m.ID, rb.SessionID)
	grantFixture(t, h, m.ID, rb.SessionID)
	f := &fakeAdmin{failRemove: true}
	h.hub.MediaAdmin = f
	op := controlInput()
	path := "/meetings/" + m.ID + "/members/" + b.User.ID + "/remove"
	s, r := h.call("POST", path, a.Token, op)
	check(t, s, r, 502, "MEDIA_CONTROL_FAILED")
	cb.wait("error")
	for _, suffix := range []string{"resume", "media-token", "ws-ticket"} {
		body := any(realtime.SessionInput{SessionID: rb.SessionID})
		if suffix == "resume" {
			body = realtime.ResumeInput{SessionID: rb.SessionID, ResumeCredential: rb.ResumeCredential}
		}
		s, r = h.call("POST", "/meetings/"+m.ID+"/"+suffix, b.Token, body)
		check(t, s, r, 403, "REMOVED_FROM_MEETING")
	}
	s, r = h.call("POST", "/meetings/"+m.ID+"/join", b.Token, meeting.JoinInput{ClientRequestID: uuid.NewString()})
	check(t, s, r, 403, "REMOVED_FROM_MEETING")
	s, r = h.call("GET", "/meetings/"+m.ID+"/controls", a.Token, nil)
	check(t, s, r, 200, "OK")
	// Retried removal must also invalidate tokens refreshed after the original attempt.
	if _, e := h.db.Exec("UPDATE moderation_operations SET created_at=created_at-120000 WHERE id=?", op.RequestID); e != nil {
		t.Fatal(e)
	}
	f.failRemove = false
	f.entered = make(chan struct{}, 1)
	f.release = make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); h.call("POST", path, a.Token, op) }()
	select {
	case <-f.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream not reached")
	}
	ca.send("ping", map[string]any{})
	ca.wait("pong") // no realtime lock held over API call
	close(f.release)
	<-done
	f.entered = nil
	f.release = nil
	s, r = h.call("POST", path, a.Token, op)
	check(t, s, r, 200, "OK")
	if f.removed != 2 || f.cutoff < time.Now().Add(-time.Minute).Unix() {
		t.Fatal("bad revoke cutoff or duplicate remove", f.removed, f.cutoff)
	}
	var open int
	h.db.QueryRow("SELECT COUNT(*) FROM attendance_intervals WHERE participant_session_id=? AND left_at IS NULL", rb.SessionID).Scan(&open)
	if open != 0 {
		t.Fatal("attendance still open")
	}
}
func TestEndFailureRetainsEndingAndStartupRetryCompletes(t *testing.T) {
	h := newWSHarness(t)
	a, b := h.user("EndHost"), h.user("EndGuest")
	m := h.create(a.Token, "结束与恢复")
	ra, rb := h.reserve(a.Token, m.ID), h.reserve(b.Token, m.ID)
	ca, cb := h.connect(a.Token, m.ID, ra.SessionID), h.connect(b.Token, m.ID, rb.SessionID)
	grantFixture(t, h, m.ID, ra.SessionID)
	grantFixture(t, h, m.ID, rb.SessionID)
	f := &fakeAdmin{failDelete: true}
	h.hub.MediaAdmin = f
	path := "/meetings/" + m.ID + "/end"
	op := controlInput()
	s, r := h.call("POST", path, a.Token, op)
	check(t, s, r, 502, "MEDIA_CONTROL_FAILED")
	ca.wait("error")
	cb.wait("error")
	var status string
	h.db.QueryRow("SELECT status FROM meetings WHERE id=?", m.ID).Scan(&status)
	if status != "ending" {
		t.Fatal(status)
	}
	for _, token := range []string{a.Token, b.Token} {
		s, r = h.call("POST", "/meetings/"+m.ID+"/join", token, meeting.JoinInput{ClientRequestID: uuid.NewString()})
		check(t, s, r, 409, "MEETING_ENDING")
	}
	s, r = h.call("POST", "/meetings/"+m.ID+"/media-token", a.Token, realtime.SessionInput{SessionID: ra.SessionID})
	check(t, s, r, 409, "MEETING_ENDING")
	h.hub.Shutdown()
	recovered := realtime.New(h.db, h.c)
	restoredAdmin := &fakeAdmin{}
	recovered.MediaAdmin = restoredAdmin
	defer recovered.Shutdown()
	if e := recovered.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := recovered.RetryControls(context.Background()); e != nil {
		t.Fatal(e)
	}
	if restoredAdmin.removed != 0 || restoredAdmin.deleted != 1 {
		t.Fatal("restart repeated completed removals", restoredAdmin.removed, restoredAdmin.deleted)
	}
	h.db.QueryRow("SELECT status FROM meetings WHERE id=?", m.ID).Scan(&status)
	if status != "ended" {
		t.Fatal(status)
	}
	h.handler = Router(h.db, h.c, recovered)
	s, r = h.call("POST", path, a.Token, op)
	check(t, s, r, 200, "OK") // completed idempotency survives process replacement
	var n int
	h.db.QueryRow("SELECT COUNT(*) FROM attendance_intervals WHERE left_at IS NULL").Scan(&n)
	if n != 0 {
		t.Fatal("unclosed attendance")
	}
	s, r = h.call("POST", "/meetings/"+m.ID+"/join", b.Token, meeting.JoinInput{ClientRequestID: uuid.NewString()})
	check(t, s, r, 409, "MEETING_ENDED")
}
func TestChatOnlyEndNeedsNoMediaConfiguration(t *testing.T) {
	h := newWSHarness(t)
	a := h.user("TextEndHost")
	m := h.create(a.Token, "纯文字结束")
	rsv := h.reserve(a.Token, m.ID)
	h.connect(a.Token, m.ID, rsv.SessionID)
	s, r := h.call("POST", "/meetings/"+m.ID+"/end", a.Token, controlInput())
	check(t, s, r, 200, "OK")
}

func TestRetryEndMakesDurableProgressAcrossAttemptTimeout(t *testing.T) {
	h := newWSHarness(t)
	a := h.user("ProgressHost")
	m := h.create(a.Token, "部分结束进度")
	r := h.reserve(a.Token, m.ID)
	h.connect(a.Token, m.ID, r.SessionID)
	grantFixture(t, h, m.ID, r.SessionID)
	// Three potentially valid media identities, including already-left sessions.
	for i := 0; i < 2; i++ {
		uid := h.user(fmt.Sprintf("ProgressGuest%d", i))
		rv := h.reserve(uid.Token, m.ID)
		h.connect(uid.Token, m.ID, rv.SessionID)
		grantFixture(t, h, m.ID, rv.SessionID)
	}
	f := &fakeAdmin{latency: 80 * time.Millisecond}
	h.hub.MediaAdmin = f
	who, e := (&auth.Service{DB: h.db, Config: h.c}).Authenticate(context.Background(), a.Token)
	if e != nil {
		t.Fatal(e)
	}
	op := controlInput()
	attempt, cancel := context.WithTimeout(context.Background(), 240*time.Millisecond)
	_, e = h.hub.Control(attempt, who, m.ID, "end", "", op)
	cancel()
	if e == nil {
		t.Fatal("expected partial attempt timeout")
	}
	attempt, cancel = context.WithTimeout(context.Background(), 240*time.Millisecond)
	defer cancel()
	_, e = h.hub.Control(attempt, who, m.ID, "end", "", op)
	if e != nil {
		t.Fatalf("retry repeated completed work instead of progressing: %v", e)
	}
	var status string
	h.db.QueryRow("SELECT status FROM meetings WHERE id=?", m.ID).Scan(&status)
	if status != "ended" {
		t.Fatal(status)
	}
}

func TestCleanupDiagnosticsClassifiesAndRedactsUpstream(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		err        error
	}{
		{"authorization", "UPSTREAM_AUTH", twirp.NewError(twirp.PermissionDenied, "SENSITIVE_KEY_AND_TOKEN")},
		{"timeout", "TIMEOUT", context.DeadlineExceeded},
		{"service", "UPSTREAM_UNAVAILABLE", twirp.NewError(twirp.Unavailable, "SENSITIVE_KEY_AND_TOKEN")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWSHarness(t)
			a := h.user("DiagnosticHost")
			m := h.create(a.Token, "脱敏诊断")
			rsv := h.reserve(a.Token, m.ID)
			h.connect(a.Token, m.ID, rsv.SessionID)
			grantFixture(t, h, m.ID, rsv.SessionID)
			f := &fakeAdmin{removeErr: tc.err}
			h.hub.MediaAdmin = f
			path := "/meetings/" + m.ID + "/end"
			op := controlInput()
			status, r := h.call("POST", path, a.Token, op)
			check(t, status, r, 502, "MEDIA_CONTROL_FAILED")
			if strings.Contains(r.Message, "SENSITIVE") || !strings.Contains(r.Message, "诊断编号") || !strings.Contains(r.Message, "remove") {
				t.Fatal("unsafe or missing diagnostic", r.Message)
			}
			var code, step, id, message string
			h.db.QueryRow("SELECT last_error_code,last_error_step,last_diagnostic_id,last_error_message FROM moderation_operations WHERE id=?", op.RequestID).Scan(&code, &step, &id, &message)
			if code != tc.code || step != "remove" || len(id) != 36 || strings.Contains(message, "SENSITIVE") {
				t.Fatal(code, step, id)
			}
			f.removeErr = nil
			status, r = h.call("POST", path, a.Token, op)
			check(t, status, r, 200, "OK")
		})
	}
}
func TestCleanupSkipsExpiredHistoryButIncludesLiveUpstreamIdentity(t *testing.T) {
	h := newWSHarness(t)
	a := h.user("HistoricalHost")
	m := h.create(a.Token, "历史身份清理")
	r := h.reserve(a.Token, m.ID)
	h.connect(a.Token, m.ID, r.SessionID)
	grantFixture(t, h, m.ID, r.SessionID)
	h.call("POST", "/meetings/"+m.ID+"/leave", a.Token, realtime.SessionInput{SessionID: r.SessionID})
	h.db.Exec("UPDATE media_grants SET expires_at=?", time.Now().Add(-time.Hour).UnixMilli())
	h.db.Exec("UPDATE participant_sessions SET left_at=?", time.Now().Add(-time.Hour).UnixMilli())
	f := &fakeAdmin{}
	h.hub.MediaAdmin = f
	status, reply := h.call("POST", "/meetings/"+m.ID+"/end", a.Token, controlInput())
	check(t, status, reply, 200, "OK")
	if f.removed != 0 || f.deleted != 1 {
		t.Fatal("expired absent history replayed", f.removed, f.deleted)
	}
	// Expiry alone must never hide a currently connected upstream participant.
	m = h.create(a.Token, "上游仍在线")
	r = h.reserve(a.Token, m.ID)
	h.connect(a.Token, m.ID, r.SessionID)
	grantFixture(t, h, m.ID, r.SessionID)
	h.call("POST", "/meetings/"+m.ID+"/leave", a.Token, realtime.SessionInput{SessionID: r.SessionID})
	h.db.Exec("UPDATE media_grants SET expires_at=?", time.Now().Add(-time.Hour).UnixMilli())
	h.db.Exec("UPDATE participant_sessions SET left_at=?", time.Now().Add(-time.Hour).UnixMilli())
	f.listed = []string{r.SessionID}
	status, reply = h.call("POST", "/meetings/"+m.ID+"/end", a.Token, controlInput())
	check(t, status, reply, 200, "OK")
	if f.removed != 1 {
		t.Fatal("live upstream identity omitted")
	}
}
func TestCleanupDatabaseFailureDoesNotFakeEndedAndCanRetry(t *testing.T) {
	h := newWSHarness(t)
	a := h.user("FinalizeHost")
	m := h.create(a.Token, "最终落库失败")
	_, e := h.db.Exec(`CREATE TRIGGER fail_finalize BEFORE UPDATE OF state ON moderation_operations WHEN NEW.state='completed' BEGIN SELECT RAISE(ABORT,'SENSITIVE_DATABASE_DETAIL'); END;`)
	if e != nil {
		t.Fatal(e)
	}
	op := controlInput()
	path := "/meetings/" + m.ID + "/end"
	status, r := h.call("POST", path, a.Token, op)
	check(t, status, r, 502, "MEDIA_CONTROL_FAILED")
	var state string
	h.db.QueryRow("SELECT status FROM meetings WHERE id=?", m.ID).Scan(&state)
	if state != "ending" || strings.Contains(r.Message, "SENSITIVE") {
		t.Fatal(state, r.Message)
	}
	h.db.Exec("DROP TRIGGER fail_finalize")
	status, r = h.call("POST", path, a.Token, op)
	check(t, status, r, 200, "OK")
}
func TestConcurrentEndRequestsShareCheckpoints(t *testing.T) {
	h := newWSHarness(t)
	a := h.user("ConcurrentEndHost")
	m := h.create(a.Token, "并发结束")
	rsv := h.reserve(a.Token, m.ID)
	h.connect(a.Token, m.ID, rsv.SessionID)
	grantFixture(t, h, m.ID, rsv.SessionID)
	f := &fakeAdmin{latency: 5 * time.Millisecond}
	h.hub.MediaAdmin = f
	op := controlInput()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, r := h.call("POST", "/meetings/"+m.ID+"/end", a.Token, op)
			if s != 200 {
				t.Errorf("concurrent end %d %s", s, r.Code)
			}
		}()
	}
	wg.Wait()
	if f.removed != 1 || f.deleted != 1 {
		t.Fatal("duplicate completed cleanup", f.removed, f.deleted)
	}
}
