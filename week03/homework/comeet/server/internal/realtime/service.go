package realtime

import (
	"comeet/server/internal/apperr"
	"comeet/server/internal/auth"
	"comeet/server/internal/config"
	"comeet/server/internal/meeting"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"github.com/google/uuid"
	"sync"
	"time"
)

const Grace = 45 * time.Second
const TicketTTL = 30 * time.Second
const QueueSize = 64

type Service struct {
	DB      *sql.DB
	Config  config.Config
	mu      sync.Mutex
	peers   map[string]*peer
	tickets map[[32]byte]ticket
	closed  bool
	cancel  context.CancelFunc
	done    chan struct{}
}
type ticket struct {
	Identity             auth.Identity
	MeetingID, SessionID string
	ExpiresAt            int64
}
type reservation struct {
	ID, MeetingID, UserID, SID, State string
	Generation, ReservedUntil         int64
	JoinedAt, DisconnectedAt          sql.NullInt64
	Hash                              []byte
	AuthExpiry                        int64
	Revoked                           sql.NullInt64
	MeetingStatus                     string
	Banned                            bool
}
type ResumeInput struct {
	SessionID        string `json:"sessionId"`
	ResumeCredential string `json:"resumeCredential"`
}
type SessionInput struct {
	SessionID string `json:"sessionId"`
}
type ResumeResult struct {
	SessionID string `json:"sessionId"`
	State     string `json:"state"`
	ExpiresAt int64  `json:"expiresAt"`
}
type TicketResult struct {
	Ticket    string `json:"ticket"`
	ExpiresAt int64  `json:"expiresAt"`
}

func New(db *sql.DB, c config.Config) *Service {
	return &Service{DB: db, Config: c, peers: map[string]*peer{}, tickets: map[[32]byte]ticket{}}
}
func (s *Service) load(ctx context.Context, id string) (reservation, error) {
	var v reservation
	var banned int
	e := s.DB.QueryRowContext(ctx, `SELECT p.id,p.meeting_id,p.user_id,p.auth_session_id,p.state,p.generation,p.reserved_until,p.joined_at,p.disconnected_at,p.resume_hash,a.expires_at,a.revoked_at,m.status,EXISTS(SELECT 1 FROM meeting_bans b WHERE b.meeting_id=p.meeting_id AND b.user_id=p.user_id) FROM participant_sessions p JOIN auth_sessions a ON a.id=p.auth_session_id JOIN meetings m ON m.id=p.meeting_id WHERE p.id=?`, id).Scan(&v.ID, &v.MeetingID, &v.UserID, &v.SID, &v.State, &v.Generation, &v.ReservedUntil, &v.JoinedAt, &v.DisconnectedAt, &v.Hash, &v.AuthExpiry, &v.Revoked, &v.MeetingStatus, &banned)
	v.Banned = banned != 0
	if errors.Is(e, sql.ErrNoRows) {
		return v, apperr.New(409, "SESSION_EXPIRED", "参会会话不存在，请重新加入")
	}
	return v, e
}
func (s *Service) allowed(v reservation, id auth.Identity, meetingID string, now int64) error {
	if v.UserID != id.User.ID || v.SID != id.SID || v.MeetingID != meetingID {
		return apperr.New(403, "FORBIDDEN", "参会会话不属于当前登录或会议")
	}
	if v.Revoked.Valid || v.AuthExpiry <= now || (id.ExpiresAt > 0 && id.ExpiresAt <= now) {
		return apperr.New(401, "SESSION_EXPIRED", "登录已失效，请重新登录")
	}
	if v.Banned {
		return apperr.New(403, "REMOVED_FROM_MEETING", "你已被移出本场会议")
	}
	if v.MeetingStatus != "active" {
		if v.MeetingStatus == "ending" {
			return apperr.New(409, "MEETING_ENDING", "会议正在结束")
		}
		return apperr.New(409, "MEETING_ENDED", "会议已结束")
	}
	if v.State == "left" || (v.State == "joining" && v.ReservedUntil <= now) || (v.State == "reconnecting" && (!v.DisconnectedAt.Valid || v.DisconnectedAt.Int64+Grace.Milliseconds() <= now)) {
		return apperr.New(409, "SESSION_EXPIRED", "参会恢复时间已过，请重新加入")
	}
	return nil
}
func (s *Service) Resume(ctx context.Context, id auth.Identity, meetingID string, in ResumeInput) (ResumeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ResumeResult{}, apperr.New(503, "SERVER_RESTART", "业务服务正在重启")
	}
	v, e := s.load(ctx, in.SessionID)
	if e != nil {
		return ResumeResult{}, e
	}
	if e = s.allowed(v, id, meetingID, time.Now().UnixMilli()); e != nil {
		return ResumeResult{}, e
	}
	hash := sha256.Sum256([]byte(in.ResumeCredential))
	if len(in.ResumeCredential) > 256 || subtle.ConstantTimeCompare(hash[:], v.Hash) != 1 {
		return ResumeResult{}, apperr.New(403, "FORBIDDEN", "恢复凭证无效")
	}
	if p := s.peers[v.ID]; p != nil {
		if p.ctx.Err() == nil {
			return ResumeResult{}, apperr.New(409, "ALREADY_JOINED", "原连接仍在线，请返回原标签页或等待断线确认")
		}
		s.disconnectLocked(p)
		v, e = s.load(ctx, in.SessionID)
		if e != nil {
			return ResumeResult{}, e
		}
	}
	expiry := v.ReservedUntil
	if v.State == "reconnecting" {
		expiry = v.DisconnectedAt.Int64 + Grace.Milliseconds()
	}
	return ResumeResult{v.ID, v.State, expiry}, nil
}
func (s *Service) Ticket(ctx context.Context, id auth.Identity, meetingID string, in SessionInput) (TicketResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return TicketResult{}, apperr.New(503, "SERVER_RESTART", "业务服务正在重启")
	}
	now := time.Now().UnixMilli()
	v, e := s.load(ctx, in.SessionID)
	if e != nil {
		return TicketResult{}, e
	}
	if e = s.allowed(v, id, meetingID, now); e != nil {
		return TicketResult{}, e
	}
	if p := s.peers[v.ID]; p != nil {
		if p.ctx.Err() == nil {
			return TicketResult{}, apperr.New(409, "ALREADY_JOINED", "原连接仍在线，不能重复连接")
		}
		s.disconnectLocked(p)
	}
	for k, t := range s.tickets {
		if t.ExpiresAt <= now || t.SessionID == v.ID {
			delete(s.tickets, k)
		}
	}
	if len(s.tickets) >= 4096 {
		return TicketResult{}, apperr.New(503, "BUSY", "票据请求过多，请稍后重试")
	}
	raw := make([]byte, 32)
	if _, e = rand.Read(raw); e != nil {
		return TicketResult{}, e
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	deadline := now + TicketTTL.Milliseconds()
	if id.ExpiresAt > 0 && id.ExpiresAt < deadline {
		deadline = id.ExpiresAt
	}
	if v.AuthExpiry < deadline {
		deadline = v.AuthExpiry
	}
	id.ExpiresAt = minPositive(id.ExpiresAt, v.AuthExpiry)
	s.tickets[sha256.Sum256([]byte(value))] = ticket{id, meetingID, v.ID, deadline}
	return TicketResult{value, deadline}, nil
}
func minPositive(a, b int64) int64 {
	if a <= 0 || b < a {
		return b
	}
	return a
}
func (s *Service) consume(ctx context.Context, value string) (ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := sha256.Sum256([]byte(value))
	t, ok := s.tickets[key]
	delete(s.tickets, key)
	if !ok || len(value) > 256 || t.ExpiresAt <= time.Now().UnixMilli() {
		return ticket{}, apperr.New(401, "TICKET_INVALID", "连接票据已失效，请重新申请")
	}
	v, e := s.load(ctx, t.SessionID)
	if e != nil {
		return ticket{}, e
	}
	if e = s.allowed(v, t.Identity, t.MeetingID, time.Now().UnixMilli()); e != nil {
		return ticket{}, e
	}
	return t, nil
}
func (s *Service) Recover(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	// Online records from a dead process get one recovery grace; already disconnected
	// records retain their original deadline, never an indefinitely extended grace.
	_, e := s.DB.ExecContext(ctx, "UPDATE participant_sessions SET state='reconnecting',disconnected_at=? WHERE state='online'", now)
	if e != nil {
		return e
	}
	return s.sweepLocked(ctx, now)
}
func (s *Service) Start() {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.mu.Lock()
				s.sweepLocked(ctx, time.Now().UnixMilli())
				s.mu.Unlock()
			}
		}
	}()
}
func (s *Service) Shutdown() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	now := time.Now().UnixMilli()
	ctx, release := context.WithTimeout(context.Background(), 5*time.Second)
	defer release()
	var waiting []<-chan struct{}
	for id, p := range s.peers {
		s.DB.ExecContext(ctx, "UPDATE participant_sessions SET state='reconnecting',disconnected_at=? WHERE id=? AND generation=? AND state='online'", now, id, p.generation)
		delete(s.peers, id)
		p.stop("SERVER_RESTART", "业务服务重启中，请稍候重连", false)
		if p.done != nil {
			waiting = append(waiting, p.done)
		}
	}
	s.tickets = map[[32]byte]ticket{}
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for _, connection := range waiting {
		select {
		case <-connection:
		case <-timer.C:
			return
		}
	}
}
func (s *Service) sweepLocked(ctx context.Context, now int64) error {
	// A transport that vanished before its close callback must not remain online.
	online, e := s.DB.QueryContext(ctx, "SELECT id,last_seen_at FROM participant_sessions WHERE state='online'")
	if e != nil {
		return e
	}
	type seen struct {
		id string
		at int64
	}
	var active []seen
	for online.Next() {
		var row seen
		var at sql.NullInt64
		if e = online.Scan(&row.id, &at); e != nil {
			online.Close()
			return e
		}
		row.at = at.Int64
		active = append(active, row)
	}
	e = online.Err()
	online.Close()
	if e != nil {
		return e
	}
	for _, row := range active {
		p := s.peers[row.id]
		if p == nil {
			v, e := s.load(ctx, row.id)
			if e != nil {
				return e
			}
			if _, e = s.DB.ExecContext(ctx, "UPDATE participant_sessions SET state='reconnecting',disconnected_at=? WHERE id=? AND state='online'", now, row.id); e != nil {
				return e
			}
			s.membersChangedLocked(v.MeetingID, "member.updated")
		} else if p.ctx.Err() != nil || row.at < now-30_000 {
			p.cancel()
			s.disconnectLocked(p)
		}
	}
	for key, t := range s.tickets {
		if t.ExpiresAt <= now {
			delete(s.tickets, key)
		}
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT p.id FROM participant_sessions p JOIN auth_sessions a ON a.id=p.auth_session_id JOIN meetings m ON m.id=p.meeting_id WHERE p.state!='left' AND ((p.state='joining' AND p.reserved_until<=?) OR (p.state='reconnecting' AND p.disconnected_at<=?) OR a.expires_at<=? OR a.revoked_at IS NOT NULL OR m.status!='active' OR EXISTS(SELECT 1 FROM meeting_bans b WHERE b.meeting_id=p.meeting_id AND b.user_id=p.user_id))`, now, now-Grace.Milliseconds(), now)
	if e != nil {
		return e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		v, e := s.load(ctx, id)
		if e != nil {
			return e
		}
		code, message := "SESSION_EXPIRED", "参会会话已过期，请重新加入"
		if v.AuthExpiry <= now || v.Revoked.Valid {
			message = "登录已失效，请重新登录"
		} else if v.Banned {
			code, message = "REMOVED_FROM_MEETING", "你已被移出本场会议"
		} else if v.MeetingStatus != "active" {
			code, message = "MEETING_ENDED", "会议已结束"
		}
		if e = s.endLocked(ctx, v, code, message); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) endLocked(ctx context.Context, v reservation, code, message string) error {
	now := time.Now().UnixMilli()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "UPDATE participant_sessions SET state='left',left_at=COALESCE(left_at,?) WHERE id=? AND state!='left'", now, v.ID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE attendance_intervals SET left_at=? WHERE participant_session_id=? AND left_at IS NULL", now, v.ID); e != nil {
		return e
	}
	if v.JoinedAt.Valid {
		if _, e = tx.ExecContext(ctx, "UPDATE meeting_members SET last_left_at=? WHERE meeting_id=? AND user_id=?", now, v.MeetingID, v.UserID); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if p := s.peers[v.ID]; p != nil {
		delete(s.peers, v.ID)
		p.stop(code, message, true)
	}
	s.membersChangedLocked(v.MeetingID, "member.left")
	return nil
}
func (s *Service) Leave(ctx context.Context, id auth.Identity, meetingID string, in SessionInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, e := s.load(ctx, in.SessionID)
	if e != nil {
		return e
	}
	if v.UserID != id.User.ID || v.SID != id.SID || v.MeetingID != meetingID {
		return apperr.New(403, "FORBIDDEN", "只能离开本人的参会会话")
	}
	if v.State == "left" {
		return nil
	}
	return s.endLocked(ctx, v, "LEFT_MEETING", "已离开会议")
}
func (s *Service) RevokeSID(ctx context.Context, sid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, e := s.DB.QueryContext(ctx, "SELECT id FROM participant_sessions WHERE auth_session_id=? AND state IN ('online','reconnecting')", sid)
	if e != nil {
		return e
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		v, e := s.load(ctx, id)
		if e != nil {
			return e
		}
		if e = s.endLocked(ctx, v, "SESSION_EXPIRED", "登录已退出，请重新登录"); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) detail(ctx context.Context, id auth.Identity, meetingID string) (meeting.Meeting, error) {
	return (&meeting.Service{DB: s.DB, Config: s.Config}).Detail(ctx, id, meetingID)
}
func validUUID(value string) bool { _, e := uuid.Parse(value); return e == nil }
