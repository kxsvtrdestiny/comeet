package realtime

import (
	"comeet/server/internal/meeting"
	"context"
	"github.com/google/uuid"
	"testing"
	"time"
)

func statusOf(t *testing.T, s *Service, id string) string {
	t.Helper()
	var v string
	if e := s.DB.QueryRow("SELECT status FROM meetings WHERE id=?", id).Scan(&v); e != nil {
		t.Fatal(e)
	}
	return v
}
func TestEmptyScheduleSurvivesRestartAndExpiresWithoutSleep(t *testing.T) {
	s, _, m, r := fixture(t)
	s.Config.EmptySeconds = 2
	now := time.Now()
	s.DB.Exec("UPDATE participant_sessions SET state='left' WHERE id=?", r.SessionID)
	s.DB.Exec("UPDATE meetings SET last_empty_at=? WHERE id=?", now.Add(-time.Second).UnixMilli(), m.ID)
	if e := s.Schedule(context.Background(), now); e != nil {
		t.Fatal(e)
	}
	if statusOf(t, s, m.ID) != "active" {
		t.Fatal("ended too early")
	}
	next := New(s.DB, s.Config)
	defer next.Shutdown()
	if e := next.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := next.Schedule(context.Background(), now.Add(2*time.Second)); e != nil {
		t.Fatal(e)
	}
	if statusOf(t, next, m.ID) != "ended" {
		t.Fatal("empty deadline reset on restart")
	}
	if e := next.Schedule(context.Background(), now.Add(3*time.Second)); e != nil {
		t.Fatal(e)
	}
	var n int
	s.DB.QueryRow("SELECT COUNT(*) FROM moderation_operations WHERE meeting_id=?", m.ID).Scan(&n)
	if n != 1 {
		t.Fatal("duplicate auto end", n)
	}
}
func TestSchedulePreservesReconnectGraceAndValidReservation(t *testing.T) {
	s, id, m, r := fixture(t)
	s.Config.EmptySeconds = 1
	now := time.Now()
	s.DB.Exec("UPDATE meetings SET last_empty_at=? WHERE id=?", now.Add(-time.Hour).UnixMilli(), m.ID)
	if e := s.Schedule(context.Background(), now); e != nil {
		t.Fatal(e)
	}
	if statusOf(t, s, m.ID) != "active" {
		t.Fatal("valid reservation ended")
	}
	p := unitPeer(t, id, m.ID, r.SessionID)
	if e := s.attach(p, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	p.cancel()
	s.disconnect(p)
	if e := s.Schedule(context.Background(), now.Add(30*time.Second)); e != nil {
		t.Fatal(e)
	}
	if statusOf(t, s, m.ID) != "active" {
		t.Fatal("grace counted as empty")
	}
	if e := s.Schedule(context.Background(), now.Add(46*time.Second)); e != nil {
		t.Fatal(e)
	}
	if statusOf(t, s, m.ID) != "active" {
		t.Fatal("empty timer should start after grace")
	}
	if e := s.Schedule(context.Background(), now.Add(48*time.Second)); e != nil {
		t.Fatal(e)
	}
	if statusOf(t, s, m.ID) != "ended" {
		t.Fatal("never ended after grace+empty deadline")
	}
}
func TestMaximumScheduleEndsOccupiedMeetingAndRejectsReentry(t *testing.T) {
	s, id, m, r := fixture(t)
	s.Config.MaxSeconds = 1
	p := unitPeer(t, id, m.ID, r.SessionID)
	if e := s.attach(p, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if e := s.Schedule(context.Background(), time.Now().Add(2*time.Second)); e != nil {
		t.Fatal(e)
	}
	if statusOf(t, s, m.ID) != "ended" {
		t.Fatal("maximum duration ignored")
	}
	if _, e := (&meeting.Service{DB: s.DB, Config: s.Config}).Join(context.Background(), id, m.ID, meeting.JoinInput{ClientRequestID: uuid.NewString()}); e == nil {
		t.Fatal("ended meeting admitted user")
	}
	var n int
	s.DB.QueryRow("SELECT COUNT(*) FROM attendance_intervals WHERE left_at IS NULL").Scan(&n)
	if n != 0 {
		t.Fatal("attendance left open")
	}
}
