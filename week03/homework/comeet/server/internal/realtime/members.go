package realtime

import (
	"comeet/server/internal/apperr"
	"comeet/server/internal/auth"
	"context"
	"github.com/google/uuid"
	"time"
)

type Member struct {
	UserID      string `json:"userId"`
	SessionID   string `json:"sessionId"`
	DisplayName string `json:"displayName"`
	State       string `json:"state"`
	IsHost      bool   `json:"isHost"`
}

func (s *Service) members(ctx context.Context, meetingID string) ([]Member, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT p.user_id,p.id,mm.display_name_snapshot,p.state,p.user_id=m.host_user_id FROM participant_sessions p JOIN meetings m ON m.id=p.meeting_id JOIN meeting_members mm ON mm.meeting_id=p.meeting_id AND mm.user_id=p.user_id WHERE p.meeting_id=? AND p.state IN ('online','reconnecting') ORDER BY p.joined_at,p.id`, meetingID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	list := []Member{}
	for rows.Next() {
		var m Member
		if e = rows.Scan(&m.UserID, &m.SessionID, &m.DisplayName, &m.State, &m.IsHost); e != nil {
			return nil, e
		}
		list = append(list, m)
	}
	return list, rows.Err()
}
func (s *Service) Members(ctx context.Context, id auth.Identity, meetingID string) ([]Member, error) {
	if _, e := s.detail(ctx, id, meetingID); e != nil {
		return nil, e
	}
	return s.members(ctx, meetingID)
}
func (s *Service) membersChangedLocked(meetingID, kind string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	members, e := s.members(ctx, meetingID)
	if e != nil {
		return
	}
	s.broadcastLocked(meetingID, event(meetingID, kind, "", map[string]any{"members": members}))
}
func (s *Service) broadcastLocked(meetingID string, v Event) {
	for _, p := range s.peers {
		if p.ticket.MeetingID == meetingID {
			p.enqueue(v)
		}
	}
}
func (s *Service) attach(p *peer, requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return apperr.New(503, "SERVER_RESTART", "业务服务正在重启")
	}
	ctx := p.ctx
	t := p.ticket
	v, e := s.load(ctx, t.SessionID)
	if e != nil {
		return e
	}
	if e = s.allowed(v, t.Identity, t.MeetingID, time.Now().UnixMilli()); e != nil {
		return e
	}
	if old := s.peers[v.ID]; old != nil {
		if old.ctx.Err() == nil {
			return apperr.New(409, "ALREADY_JOINED", "原连接仍在线，不能接管")
		}
		s.disconnectLocked(old)
		v, e = s.load(ctx, t.SessionID)
		if e != nil {
			return e
		}
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	var name string
	if e = tx.QueryRowContext(ctx, "SELECT display_name FROM users WHERE id=?", v.UserID).Scan(&name); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO meeting_members(id,meeting_id,user_id,display_name_snapshot,first_joined_at) VALUES (?,?,?,?,?) ON CONFLICT(meeting_id,user_id) DO UPDATE SET last_left_at=NULL`, uuid.NewString(), v.MeetingID, v.UserID, name, now); e != nil {
		return e
	}
	// The attendance interval is opened only at the first real room.join, not reservation/resume.
	if !v.JoinedAt.Valid {
		if _, e = tx.ExecContext(ctx, "INSERT INTO attendance_intervals(id,participant_session_id,joined_at) VALUES (?,?,?)", uuid.NewString(), v.ID, now); e != nil {
			return e
		}
	}
	p.generation = v.Generation + 1
	p.name = name
	if _, e = tx.ExecContext(ctx, "UPDATE participant_sessions SET state='online',generation=?,joined_at=COALESCE(joined_at,?),last_seen_at=?,disconnected_at=NULL WHERE id=?", p.generation, now, now, v.ID); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	// Register first while the room lock also serializes chat writes/broadcasts;
	// then capture the high-water mark. Nothing can fall between these steps.
	s.peers[v.ID] = p
	members, e := s.members(ctx, v.MeetingID)
	if e != nil {
		delete(s.peers, v.ID)
		s.DB.Exec("UPDATE participant_sessions SET state='reconnecting',disconnected_at=? WHERE id=? AND generation=?", now, v.ID, p.generation)
		return e
	}
	var high int64
	if e = s.DB.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM chat_messages WHERE meeting_id=?", v.MeetingID).Scan(&high); e != nil {
		return e
	}
	m, e := s.detail(ctx, t.Identity, v.MeetingID)
	if e != nil {
		return e
	}
	p.enqueue(event(v.MeetingID, "room.snapshot", requestID, map[string]any{"meeting": m, "members": members, "latestMessageId": high, "sessionId": v.ID, "generation": p.generation}))
	s.membersChangedLocked(v.MeetingID, "member.joined")
	return nil
}
