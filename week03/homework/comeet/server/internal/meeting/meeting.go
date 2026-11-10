package meeting

import (
	"comeet/server/internal/apperr"
	"comeet/server/internal/auth"
	"comeet/server/internal/config"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Service struct {
	DB     *sql.DB
	Config config.Config
}
type Meeting struct {
	ID         string `json:"id"`
	MeetingNo  string `json:"meetingNo"`
	Title      string `json:"title"`
	HostUserID string `json:"hostUserId"`
	HostName   string `json:"hostName"`
	Status     string `json:"status"`
	StartedAt  int64  `json:"startedAt"`
	EndedAt    *int64 `json:"endedAt"`
}
type Preparation struct {
	ID        string `json:"id"`
	MeetingNo string `json:"meetingNo"`
	Title     string `json:"title"`
	HostName  string `json:"hostName"`
	Status    string `json:"status"`
}
type CreateInput struct {
	Title           string `json:"title"`
	ClientRequestID string `json:"clientRequestId"`
}
type JoinInput struct {
	ClientRequestID string `json:"clientRequestId"`
}
type Reservation struct {
	SessionID        string `json:"sessionId"`
	ResumeCredential string `json:"resumeCredential"`
	ExpiresAt        int64  `json:"expiresAt"`
}
type ListResult struct {
	Items      []Meeting `json:"items"`
	NextCursor string    `json:"nextCursor"`
	HasMore    bool      `json:"hasMore"`
}

const columns = `m.id,m.meeting_no,m.title,m.host_user_id,u.display_name,m.status,m.started_at,m.ended_at`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Meeting, error) {
	var m Meeting
	e := row.Scan(&m.ID, &m.MeetingNo, &m.Title, &m.HostUserID, &m.HostName, &m.Status, &m.StartedAt, &m.EndedAt)
	return m, e
}
func validID(id string) bool { _, e := uuid.Parse(id); return e == nil }
func active(status string) error {
	switch status {
	case "ended":
		return apperr.New(409, "MEETING_ENDED", "会议已结束")
	case "ending":
		return apperr.New(409, "MEETING_ENDING", "会议正在结束")
	}
	return nil
}
func (s *Service) Create(ctx context.Context, user auth.Identity, in CreateInput) (Meeting, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		in.Title = "未命名会议"
	}
	if utf8.RuneCountInString(in.Title) > 80 {
		return Meeting{}, apperr.Invalid("会议标题最多 80 个字符")
	}
	if !validID(in.ClientRequestID) {
		return Meeting{}, apperr.Invalid("clientRequestId 必须是 UUID")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Meeting{}, e
	}
	defer tx.Rollback()
	previous, e := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM meetings m JOIN users u ON u.id=m.host_user_id WHERE m.host_user_id=? AND m.client_request_id=?", user.User.ID, in.ClientRequestID))
	if e == nil {
		if previous.Title != in.Title {
			return Meeting{}, apperr.New(409, "IDEMPOTENCY_CONFLICT", "同一请求标识不能用于不同会议内容")
		}
		return previous, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return Meeting{}, e
	}
	id := uuid.NewString()
	now := time.Now().UnixMilli()
	var no string
	for attempt := 0; attempt < 8; attempt++ {
		n, e := rand.Int(rand.Reader, big.NewInt(1_000_000_000))
		if e != nil {
			return Meeting{}, e
		}
		no = fmt.Sprintf("%09d", n.Int64())
		_, e = tx.ExecContext(ctx, `INSERT INTO meetings(id,meeting_no,title,host_user_id,livekit_room_name,status,started_at,client_request_id) VALUES (?,?,?,?,?,'active',?,?)`, id, no, in.Title, user.User.ID, id, now, in.ClientRequestID)
		if e == nil {
			if e = tx.Commit(); e != nil {
				return Meeting{}, e
			}
			return Meeting{id, no, in.Title, user.User.ID, user.User.DisplayName, "active", now, nil}, nil
		}
		if !apperr.IsUnique(e) {
			return Meeting{}, e
		}
	}
	return Meeting{}, apperr.New(503, "MEETING_NUMBER_UNAVAILABLE", "会议号生成繁忙，请重试")
}
func (s *Service) Detail(ctx context.Context, user auth.Identity, id string) (Meeting, error) {
	if !validID(id) {
		return Meeting{}, apperr.New(404, "MEETING_NOT_FOUND", "会议不存在")
	}
	m, e := scan(s.DB.QueryRowContext(ctx, "SELECT "+columns+" FROM meetings m JOIN users u ON u.id=m.host_user_id WHERE m.id=?", id))
	if errors.Is(e, sql.ErrNoRows) {
		return m, apperr.New(404, "MEETING_NOT_FOUND", "会议不存在")
	}
	if e != nil {
		return m, e
	}
	if m.HostUserID != user.User.ID {
		var n int
		if e = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM meeting_members WHERE meeting_id=? AND user_id=?", id, user.User.ID).Scan(&n); e != nil {
			return m, e
		}
		if n == 0 {
			return Meeting{}, apperr.New(403, "FORBIDDEN", "只有主持人或实际参会成员可以查看详情")
		}
	}
	return m, nil
}
func (s *Service) Prepare(ctx context.Context, id string) (Preparation, error) {
	if !validID(id) {
		return Preparation{}, apperr.New(404, "MEETING_NOT_FOUND", "会议不存在")
	}
	var p Preparation
	e := s.DB.QueryRowContext(ctx, `SELECT m.id,m.meeting_no,m.title,u.display_name,m.status FROM meetings m JOIN users u ON u.id=m.host_user_id WHERE m.id=?`, id).Scan(&p.ID, &p.MeetingNo, &p.Title, &p.HostName, &p.Status)
	if errors.Is(e, sql.ErrNoRows) {
		return p, apperr.New(404, "MEETING_NOT_FOUND", "会议不存在")
	}
	if e != nil {
		return p, e
	}
	return p, active(p.Status)
}
func (s *Service) Resolve(ctx context.Context, no string) (Preparation, error) {
	no = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '-' {
			return -1
		}
		return r
	}, no)
	if len(no) != 9 || strings.IndexFunc(no, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return Preparation{}, apperr.Invalid("会议号需为 9 位数字，可包含空格或连字符")
	}
	var id string
	e := s.DB.QueryRowContext(ctx, "SELECT id FROM meetings WHERE meeting_no=?", no).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return Preparation{}, apperr.New(404, "MEETING_NOT_FOUND", "会议不存在")
	}
	if e != nil {
		return Preparation{}, e
	}
	return s.Prepare(ctx, id)
}
func (s *Service) List(ctx context.Context, user auth.Identity, status, keyword, cursor string, limit int) (ListResult, error) {
	result := ListResult{Items: []Meeting{}}
	if status != "" && status != "active" && status != "ending" && status != "ended" {
		return result, apperr.Invalid("会议状态无效")
	}
	if limit < 1 || limit > 100 || utf8.RuneCountInString(keyword) > 80 {
		return result, apperr.Invalid("分页参数或搜索长度无效")
	}
	q := "SELECT " + columns + ` FROM meetings m JOIN users u ON u.id=m.host_user_id WHERE (m.host_user_id=? OR EXISTS(SELECT 1 FROM meeting_members mm WHERE mm.meeting_id=m.id AND mm.user_id=?))`
	args := []any{user.User.ID, user.User.ID}
	if status != "" {
		q += " AND m.status=?"
		args = append(args, status)
	}
	if keyword != "" {
		q += " AND (instr(lower(m.title),lower(?))>0 OR instr(m.meeting_no,?)>0)"
		args = append(args, keyword, keyword)
	}
	if cursor != "" {
		decoded, e := base64.RawURLEncoding.DecodeString(cursor)
		parts := strings.Split(string(decoded), "|")
		if e != nil || len(parts) != 2 || !validID(parts[1]) {
			return result, apperr.Invalid("分页游标无效")
		}
		stamp, e := strconv.ParseInt(parts[0], 10, 64)
		if e != nil {
			return result, apperr.Invalid("分页游标无效")
		}
		q += " AND (m.started_at<? OR (m.started_at=? AND m.id<?))"
		args = append(args, stamp, stamp, parts[1])
	}
	q += " ORDER BY m.started_at DESC,m.id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, e := s.DB.QueryContext(ctx, q, args...)
	if e != nil {
		return result, e
	}
	defer rows.Close()
	for rows.Next() {
		m, e := scan(rows)
		if e != nil {
			return result, e
		}
		result.Items = append(result.Items, m)
	}
	if e = rows.Err(); e != nil {
		return result, e
	}
	if len(result.Items) > limit {
		result.HasMore = true
		result.Items = result.Items[:limit]
		m := result.Items[len(result.Items)-1]
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d|%s", m.StartedAt, m.ID)))
	}
	return result, nil
}

// The unpredictable session UUID plus a domain-separated HMAC permits exact retries
// without storing the raw resume credential. Only SHA-256(credential) is persisted.
func (s *Service) credential(sessionID, sid, requestID string) string {
	mac := hmac.New(sha256.New, []byte(s.Config.JWTSecret))
	mac.Write([]byte("comeet:resume:v1\x00" + sessionID + "\x00" + sid + "\x00" + requestID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Service) Join(ctx context.Context, user auth.Identity, id string, in JoinInput) (Reservation, error) {
	if !validID(id) {
		return Reservation{}, apperr.New(404, "MEETING_NOT_FOUND", "会议不存在")
	}
	if !validID(in.ClientRequestID) {
		return Reservation{}, apperr.Invalid("clientRequestId 必须是 UUID")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Reservation{}, e
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	var status string
	e = tx.QueryRowContext(ctx, "SELECT status FROM meetings WHERE id=?", id).Scan(&status)
	if errors.Is(e, sql.ErrNoRows) {
		return Reservation{}, apperr.New(404, "MEETING_NOT_FOUND", "会议不存在")
	}
	if e != nil {
		return Reservation{}, e
	}
	if e = active(status); e != nil {
		return Reservation{}, e
	}
	var valid int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_sessions WHERE id=? AND user_id=? AND revoked_at IS NULL AND expires_at>?", user.SID, user.User.ID, now).Scan(&valid); e != nil {
		return Reservation{}, e
	}
	if valid == 0 {
		return Reservation{}, apperr.New(401, "SESSION_EXPIRED", "登录已失效，请重新登录")
	}
	var banned int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM meeting_bans WHERE meeting_id=? AND user_id=?", id, user.User.ID).Scan(&banned); e != nil {
		return Reservation{}, e
	}
	if banned > 0 {
		return Reservation{}, apperr.New(403, "REMOVED_FROM_MEETING", "你已被移出本场会议")
	}
	// Expired reservations are removed from capacity and the partial unique index.
	if _, e = tx.ExecContext(ctx, "UPDATE participant_sessions SET state='left',left_at=? WHERE meeting_id=? AND state='joining' AND reserved_until<=?", now, id, now); e != nil {
		return Reservation{}, e
	}
	var sessionID, sid, state string
	var expiry int64
	e = tx.QueryRowContext(ctx, "SELECT id,auth_session_id,state,reserved_until FROM participant_sessions WHERE meeting_id=? AND user_id=? AND client_request_id=?", id, user.User.ID, in.ClientRequestID).Scan(&sessionID, &sid, &state, &expiry)
	if e == nil {
		if sid != user.SID {
			return Reservation{}, apperr.New(409, "ALREADY_JOINED", "此请求属于另一个登录会话")
		}
		if state != "joining" || expiry <= now {
			return Reservation{}, apperr.New(409, "SESSION_EXPIRED", "预留已失效，请使用新请求重新加入")
		}
		if e = tx.Commit(); e != nil {
			return Reservation{}, e
		}
		return Reservation{sessionID, s.credential(sessionID, sid, in.ClientRequestID), expiry}, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return Reservation{}, e
	}
	var count int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM participant_sessions WHERE meeting_id=? AND user_id=? AND state IN ('joining','online','reconnecting')", id, user.User.ID).Scan(&count); e != nil {
		return Reservation{}, e
	}
	if count > 0 {
		return Reservation{}, apperr.New(409, "ALREADY_JOINED", "你已预留或加入本场会议，请返回原标签页")
	}
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM participant_sessions WHERE meeting_id=? AND state IN ('joining','online','reconnecting')", id).Scan(&count); e != nil {
		return Reservation{}, e
	}
	if count >= s.Config.MaxParticipants {
		return Reservation{}, apperr.New(409, "MEETING_FULL", "会议人数已满，请稍后重试")
	}
	sessionID = uuid.NewString()
	expiry = now + 60_000
	credential := s.credential(sessionID, user.SID, in.ClientRequestID)
	hash := sha256.Sum256([]byte(credential))
	_, e = tx.ExecContext(ctx, `INSERT INTO participant_sessions(id,meeting_id,user_id,auth_session_id,resume_hash,state,reserved_until,client_request_id) VALUES (?,?,?,?,?,'joining',?,?)`, sessionID, id, user.User.ID, user.SID, hash[:], expiry, in.ClientRequestID)
	if e != nil {
		return Reservation{}, e
	}
	if e = tx.Commit(); e != nil {
		return Reservation{}, e
	}
	return Reservation{sessionID, credential, expiry}, nil
}
