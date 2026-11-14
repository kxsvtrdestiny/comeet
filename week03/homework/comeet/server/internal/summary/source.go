package summary

import (
	"comeet/server/internal/apperr"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

type Entry struct {
	ID      string `json:"id"`
	Author  string `json:"author"`
	Content string `json:"content"`
	Version int64  `json:"version,omitempty"`
	At      int64  `json:"at"`
}
type Snapshot struct {
	Title     string  `json:"title"`
	MeetingNo string  `json:"meetingNo"`
	StartedAt int64   `json:"startedAt"`
	EndedAt   *int64  `json:"endedAt"`
	Entries   []Entry `json:"entries"`
}
type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Service) snapshot(ctx context.Context, q queryer, mid string) (Snapshot, string, string, error) {
	return s.snapshotBudget(ctx, q, mid, s.maxChars(), s.maxBytes())
}
func (s *Service) snapshotBudget(ctx context.Context, q queryer, mid string, maxChars, maxBytes int) (Snapshot, string, string, error) {
	v := Snapshot{Entries: []Entry{}}
	if e := q.QueryRowContext(ctx, "SELECT title,meeting_no,started_at,ended_at FROM meetings WHERE id=?", mid).Scan(&v.Title, &v.MeetingNo, &v.StartedAt, &v.EndedAt); e != nil {
		return v, "", "", e
	}
	chars, bytes := utf8.RuneCountInString(v.Title), len(v.Title)
	add := func(e Entry) error {
		chars += utf8.RuneCountInString(e.Content) + utf8.RuneCountInString(e.Author)
		bytes += len(e.Content) + len(e.Author) + len(e.ID) + 64
		if chars > maxChars || bytes > maxBytes {
			return apperr.New(413, "SOURCE_TOO_LARGE", "记录超过本次纪要输入预算；请缩短手动笔记后重试，本版本不会静默截断聊天或自动分块")
		}
		if strings.TrimSpace(e.Content) != "" {
			v.Entries = append(v.Entries, e)
		}
		return nil
	}
	rows, e := q.QueryContext(ctx, "SELECT id,sender_name_snapshot,content,created_at FROM chat_messages WHERE meeting_id=? ORDER BY id", mid)
	if e != nil {
		return v, "", "", e
	}
	for rows.Next() {
		var x Entry
		var id int64
		if e = rows.Scan(&id, &x.Author, &x.Content, &x.At); e != nil {
			break
		}
		x.ID = fmt.Sprintf("message:%d", id)
		if e = add(x); e != nil {
			break
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return v, "", "", e
	}
	rows, e = q.QueryContext(ctx, "SELECT n.id,u.display_name,n.content,n.version,n.updated_at FROM meeting_notes n JOIN users u ON u.id=n.author_user_id WHERE n.meeting_id=? ORDER BY n.id", mid)
	if e != nil {
		return v, "", "", e
	}
	for rows.Next() {
		var x Entry
		if e = rows.Scan(&x.ID, &x.Author, &x.Content, &x.Version, &x.At); e != nil {
			break
		}
		x.ID = "note:" + x.ID
		if e = add(x); e != nil {
			break
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return v, "", "", e
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return v, "", "", e
	}
	if len(raw) > maxBytes {
		return v, "", "", apperr.New(413, "SOURCE_TOO_LARGE", "记录序列化后超过字节预算，请缩短笔记后重试；不会静默截断")
	}
	hash := sha256.Sum256(raw)
	return v, string(raw), hex.EncodeToString(hash[:]), nil
}
