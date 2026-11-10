package realtime

import (
	"comeet/server/internal/apperr"
	"comeet/server/internal/auth"
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

type ChatInput struct {
	ClientMessageID string `json:"clientMessageId"`
	Content         string `json:"content"`
}
type Message struct {
	ID              int64  `json:"id"`
	ClientMessageID string `json:"clientMessageId"`
	SenderID        string `json:"senderId"`
	SenderName      string `json:"senderName"`
	Content         string `json:"content"`
	CreatedAt       int64  `json:"createdAt"`
}
type MessagePage struct {
	Items      []Message `json:"items"`
	NextCursor int64     `json:"nextCursor"`
	HasMore    bool      `json:"hasMore"`
}

func scanMessage(row interface{ Scan(...any) error }) (Message, error) {
	var m Message
	e := row.Scan(&m.ID, &m.ClientMessageID, &m.SenderID, &m.SenderName, &m.Content, &m.CreatedAt)
	return m, e
}

const messageColumns = "id,client_message_id,sender_user_id,sender_name_snapshot,content,created_at"

func (s *Service) Messages(ctx context.Context, id auth.Identity, meetingID string, before, after int64, forward bool, limit int) (MessagePage, error) {
	result := MessagePage{Items: []Message{}}
	if _, e := s.detail(ctx, id, meetingID); e != nil {
		return result, e
	}
	if before < 0 || after < 0 || limit < 1 || limit > 100 || (before > 0 && forward) {
		return result, apperr.Invalid("消息分页参数无效")
	}
	query := "SELECT " + messageColumns + " FROM chat_messages WHERE meeting_id=?"
	args := []any{meetingID}
	if forward {
		query += " AND id>? ORDER BY id ASC"
		args = append(args, after)
	} else {
		if before > 0 {
			query += " AND id<?"
			args = append(args, before)
		}
		query += " ORDER BY id DESC"
	}
	query += " LIMIT ?"
	args = append(args, limit+1)
	rows, e := s.DB.QueryContext(ctx, query, args...)
	if e != nil {
		return result, e
	}
	defer rows.Close()
	for rows.Next() {
		m, e := scanMessage(rows)
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
	}
	if len(result.Items) > 0 {
		result.NextCursor = result.Items[len(result.Items)-1].ID
	}
	if !forward {
		for i, j := 0, len(result.Items)-1; i < j; i, j = i+1, j-1 {
			result.Items[i], result.Items[j] = result.Items[j], result.Items[i]
		}
	}
	return result, nil
}
func (s *Service) chat(p *peer, requestID string, in ChatInput) error {
	in.Content = strings.TrimSpace(in.Content)
	if !validUUID(in.ClientMessageID) || in.Content == "" || utf8.RuneCountInString(in.Content) > 2000 || len(in.Content) > 8192 {
		return apperr.Invalid("消息不能为空，最多 2000 字符且不超过 8KiB")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.peers[p.ticket.SessionID] != p || p.ctx.Err() != nil {
		return apperr.New(409, "SESSION_EXPIRED", "连接已失效")
	}
	now := time.Now()
	if now.Sub(p.chatStart) >= time.Minute {
		p.chatStart = now
		p.chatCount = 0
	}
	if p.chatCount >= 120 {
		return apperr.New(429, "RATE_LIMITED", "消息发送过于频繁，请稍后重试")
	}
	p.chatCount++
	v, e := s.load(p.ctx, p.ticket.SessionID)
	if e != nil {
		return e
	}
	if e = s.allowed(v, p.ticket.Identity, p.ticket.MeetingID, now.UnixMilli()); e != nil {
		return e
	}
	if v.State != "online" || v.Generation != p.generation {
		return apperr.New(409, "SESSION_EXPIRED", "连接已失效")
	}
	tx, e := s.DB.BeginTx(p.ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := tx.ExecContext(p.ctx, "INSERT INTO chat_messages(meeting_id,sender_user_id,sender_name_snapshot,client_message_id,content,created_at) VALUES (?,?,?,?,?,?) ON CONFLICT(meeting_id,sender_user_id,client_message_id) DO NOTHING", v.MeetingID, v.UserID, p.name, in.ClientMessageID, in.Content, now.UnixMilli())
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return e
	}
	msg, e := scanMessage(tx.QueryRowContext(p.ctx, "SELECT "+messageColumns+" FROM chat_messages WHERE meeting_id=? AND sender_user_id=? AND client_message_id=?", v.MeetingID, v.UserID, in.ClientMessageID))
	if e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if n > 0 {
		s.broadcastLocked(v.MeetingID, event(v.MeetingID, "chat.created", "", msg))
	}
	p.enqueue(event(v.MeetingID, "chat.ack", requestID, msg))
	return nil
}
