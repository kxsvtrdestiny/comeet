package realtime

import (
	"comeet/server/internal/auth"
	"comeet/server/internal/config"
	"comeet/server/internal/meeting"
	"comeet/server/internal/store"
	"context"
	"crypto/sha256"
	"github.com/google/uuid"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Service, auth.Identity, meeting.Meeting, meeting.Reservation) {
	t.Helper()
	db, e := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	c := config.Config{JWTSecret: "test-only-state-secret-0123456789abcdef", JWTIssuer: "state-test", JWTAudience: "state-web", JWTHours: 8, MaxParticipants: 6}
	a := &auth.Service{DB: db, Config: c}
	_, e = a.Register(context.Background(), auth.RegisterInput{Username: "state_user", DisplayName: "真实参会测试", Password: "State_test_password"})
	if e != nil {
		t.Fatal(e)
	}
	login, e := a.Login(context.Background(), auth.LoginInput{Username: "state_user", Password: "State_test_password"})
	if e != nil {
		t.Fatal(e)
	}
	id, e := a.Authenticate(context.Background(), login.Token)
	if e != nil {
		t.Fatal(e)
	}
	m := &meeting.Service{DB: db, Config: c}
	room, e := m.Create(context.Background(), id, meeting.CreateInput{ClientRequestID: uuid.NewString(), Title: "state"})
	if e != nil {
		t.Fatal(e)
	}
	r, e := m.Join(context.Background(), id, room.ID, meeting.JoinInput{ClientRequestID: uuid.NewString()})
	if e != nil {
		t.Fatal(e)
	}
	s := New(db, c)
	t.Cleanup(s.Shutdown)
	return s, id, room, r
}
func unitPeer(t *testing.T, id auth.Identity, m string, r string) *peer {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &peer{ticket: ticket{Identity: id, MeetingID: m, SessionID: r, ExpiresAt: time.Now().Add(TicketTTL).UnixMilli()}, ctx: ctx, cancel: cancel, out: make(chan Event, QueueSize), stopping: make(chan stopReason, 1)}
}
func TestOldCloseCannotDeleteResumedConnection(t *testing.T) {
	s, id, m, r := fixture(t)
	old := unitPeer(t, id, m.ID, r.SessionID)
	if e := s.attach(old, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	old.cancel()
	s.disconnect(old)
	if _, e := s.Resume(context.Background(), id, m.ID, ResumeInput{r.SessionID, r.ResumeCredential}); e != nil {
		t.Fatal(e)
	}
	current := unitPeer(t, id, m.ID, r.SessionID)
	if e := s.attach(current, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	s.disconnect(old)
	var state string
	var gen, intervals int
	s.DB.QueryRow("SELECT state,generation FROM participant_sessions WHERE id=?", r.SessionID).Scan(&state, &gen)
	s.DB.QueryRow("SELECT COUNT(*) FROM attendance_intervals WHERE participant_session_id=?", r.SessionID).Scan(&intervals)
	if s.peers[r.SessionID] != current || state != "online" || int64(gen) != current.generation || intervals != 1 {
		t.Fatal("stale callback corrupted resumed session", state, gen, intervals)
	}
	if e := s.chat(old, uuid.NewString(), ChatInput{uuid.NewString(), "stale"}); e == nil {
		t.Fatal("stale connection wrote chat")
	}
}
func TestSlowPeerQueueIsBoundedAndDoesNotBlockRoom(t *testing.T) {
	s, id, m, r := fixture(t)
	slow := unitPeer(t, id, m.ID, r.SessionID)
	if e := s.attach(slow, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	for len(slow.out) < cap(slow.out) {
		slow.enqueue(event(m.ID, "test", "", nil))
	}
	healthy := unitPeer(t, id, m.ID, "unit-only-second-peer")
	s.peers[healthy.ticket.SessionID] = healthy
	start := time.Now()
	s.mu.Lock()
	s.broadcastLocked(m.ID, event(m.ID, "test", "", nil))
	s.mu.Unlock()
	if time.Since(start) > time.Second || slow.ctx.Err() == nil || len(slow.out) > QueueSize || len(healthy.out) != 1 {
		t.Fatal("slow queue blocked or grew")
	}
}
func TestTicketExpiresAndIsConsumedAtomically(t *testing.T) {
	s, id, m, r := fixture(t)
	ticket, e := s.Ticket(context.Background(), id, m.ID, SessionInput{r.SessionID})
	if e != nil {
		t.Fatal(e)
	}
	if ticket.ExpiresAt > time.Now().Add(TicketTTL).UnixMilli() {
		t.Fatal("ticket ttl")
	}
	key := sha256.Sum256([]byte(ticket.Ticket))
	value := s.tickets[key]
	value.ExpiresAt = time.Now().Add(-time.Millisecond).UnixMilli()
	s.tickets[key] = value
	if _, e = s.consume(context.Background(), ticket.Ticket); e == nil {
		t.Fatal("expired ticket accepted")
	}
	if len(s.tickets) != 0 {
		t.Fatal("expired ticket retained")
	}
	ticket, e = s.Ticket(context.Background(), id, m.ID, SessionInput{r.SessionID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.consume(context.Background(), ticket.Ticket); e != nil {
		t.Fatal(e)
	}
	if _, e = s.consume(context.Background(), ticket.Ticket); e == nil {
		t.Fatal("ticket reused")
	}
}
func TestRestartRecoveryAndGraceExpiryDoNotDuplicateAttendance(t *testing.T) {
	s, id, m, r := fixture(t)
	p := unitPeer(t, id, m.ID, r.SessionID)
	if e := s.attach(p, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	s.Shutdown()
	next := New(s.DB, s.Config)
	t.Cleanup(next.Shutdown)
	if e := next.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e := next.Resume(context.Background(), id, m.ID, ResumeInput{r.SessionID, r.ResumeCredential}); e != nil {
		t.Fatal(e)
	}
	current := unitPeer(t, id, m.ID, r.SessionID)
	if e := next.attach(current, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	s.disconnect(p)
	var n int
	next.DB.QueryRow("SELECT COUNT(*) FROM attendance_intervals WHERE participant_session_id=?", r.SessionID).Scan(&n)
	if n != 1 {
		t.Fatal("restart doubled interval")
	}
	current.cancel()
	next.disconnect(current)
	next.DB.Exec("UPDATE participant_sessions SET disconnected_at=? WHERE id=?", time.Now().Add(-Grace-time.Second).UnixMilli(), r.SessionID)
	if e := next.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	var state string
	next.DB.QueryRow("SELECT state FROM participant_sessions WHERE id=?", r.SessionID).Scan(&state)
	if state != "left" {
		t.Fatal("grace extended on restart")
	}
	next.DB.QueryRow("SELECT COUNT(*) FROM attendance_intervals WHERE participant_session_id=? AND left_at IS NOT NULL", r.SessionID).Scan(&n)
	if n != 1 {
		t.Fatal("interval not closed once")
	}
	if _, e := next.Resume(context.Background(), id, m.ID, ResumeInput{r.SessionID, r.ResumeCredential}); e == nil {
		t.Fatal("expired resume accepted")
	}
}
