package meeting

import (
	"context"
	"database/sql"
	"time"
	"unicode/utf8"

	"comeet/server/internal/apperr"
	"comeet/server/internal/auth"
	"github.com/google/uuid"
)

type Attendance struct {
	SessionID      string `json:"sessionId"`
	JoinedAt       int64  `json:"joinedAt"`
	LeftAt         *int64 `json:"leftAt"`
	DurationMillis int64  `json:"durationMillis"`
}
type Attendee struct {
	UserID         string       `json:"userId"`
	DisplayName    string       `json:"displayName"`
	IsHost         bool         `json:"isHost"`
	DurationMillis int64        `json:"durationMillis"`
	Intervals      []Attendance `json:"intervals"`
}
type Record struct {
	Meeting        Meeting    `json:"meeting"`
	DurationMillis int64      `json:"durationMillis"`
	Attendees      []Attendee `json:"attendees"`
}

func (s *Service) Record(ctx context.Context, who auth.Identity, id string) (Record, error) {
	m, e := s.Detail(ctx, who, id)
	if e != nil {
		return Record{}, e
	}
	now := time.Now().UnixMilli()
	end := now
	if m.EndedAt != nil {
		end = *m.EndedAt
	}
	result := Record{Meeting: m, DurationMillis: max(0, end-m.StartedAt), Attendees: []Attendee{}}
	rows, e := s.DB.QueryContext(ctx, `SELECT mm.user_id,mm.display_name_snapshot,p.id,a.joined_at,a.left_at FROM meeting_members mm JOIN participant_sessions p ON p.meeting_id=mm.meeting_id AND p.user_id=mm.user_id JOIN attendance_intervals a ON a.participant_session_id=p.id WHERE mm.meeting_id=? ORDER BY mm.first_joined_at,mm.user_id,a.joined_at,a.id`, id)
	if e != nil {
		return result, e
	}
	defer rows.Close()
	indexes := map[string]int{}
	for rows.Next() {
		var uid, name string
		var a Attendance
		if e = rows.Scan(&uid, &name, &a.SessionID, &a.JoinedAt, &a.LeftAt); e != nil {
			return result, e
		}
		until := end
		if a.LeftAt != nil {
			until = min(until, *a.LeftAt)
		}
		a.DurationMillis = max(0, until-a.JoinedAt)
		n, ok := indexes[uid]
		if !ok {
			n = len(result.Attendees)
			indexes[uid] = n
			result.Attendees = append(result.Attendees, Attendee{UserID: uid, DisplayName: name, IsHost: uid == m.HostUserID, Intervals: []Attendance{}})
		}
		result.Attendees[n].DurationMillis += a.DurationMillis
		result.Attendees[n].Intervals = append(result.Attendees[n].Intervals, a)
	}
	return result, rows.Err()
}

type Note struct {
	ID         string `json:"id"`
	AuthorID   string `json:"authorId"`
	AuthorName string `json:"authorName"`
	Content    string `json:"content"`
	Version    int64  `json:"version"`
	UpdatedAt  int64  `json:"updatedAt"`
}
type NoteInput struct {
	Content string `json:"content"`
	Version int64  `json:"version"`
}

const noteColumns = "n.id,n.author_user_id,u.display_name,n.content,n.version,n.updated_at"

func scanNote(row interface{ Scan(...any) error }) (n Note, e error) {
	e = row.Scan(&n.ID, &n.AuthorID, &n.AuthorName, &n.Content, &n.Version, &n.UpdatedAt)
	return
}
func (s *Service) Notes(ctx context.Context, who auth.Identity, id string) ([]Note, error) {
	if _, e := s.Detail(ctx, who, id); e != nil {
		return nil, e
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT "+noteColumns+" FROM meeting_notes n JOIN users u ON u.id=n.author_user_id WHERE n.meeting_id=? ORDER BY n.updated_at,n.id", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	notes := []Note{}
	for rows.Next() {
		n, e := scanNote(rows)
		if e != nil {
			return nil, e
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}
func (s *Service) SaveNote(ctx context.Context, who auth.Identity, id string, in NoteInput) (Note, error) {
	if _, e := s.Detail(ctx, who, id); e != nil {
		return Note{}, e
	}
	if in.Version < 0 || utf8.RuneCountInString(in.Content) > 10000 || len(in.Content) > 40000 {
		return Note{}, apperr.Invalid("笔记最多10000字符/40000字节，版本不能为负数")
	}
	now := time.Now().UnixMilli()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Note{}, e
	}
	defer tx.Rollback()
	var r sql.Result
	if in.Version == 0 {
		r, e = tx.ExecContext(ctx, "INSERT INTO meeting_notes(id,meeting_id,author_user_id,content,version,updated_at) VALUES (?,?,?,?,1,?) ON CONFLICT(meeting_id,author_user_id) DO NOTHING", uuid.NewString(), id, who.User.ID, in.Content, now)
	} else {
		r, e = tx.ExecContext(ctx, "UPDATE meeting_notes SET content=?,version=version+1,updated_at=? WHERE meeting_id=? AND author_user_id=? AND version=?", in.Content, now, id, who.User.ID, in.Version)
	}
	if e != nil {
		return Note{}, e
	}
	count, e := r.RowsAffected()
	if e != nil {
		return Note{}, e
	}
	if count != 1 {
		return Note{}, apperr.New(409, "NOTE_VERSION_CONFLICT", "笔记已在其他标签页更新；本地内容保留，请比较后再保存")
	}
	n, e := scanNote(tx.QueryRowContext(ctx, "SELECT "+noteColumns+" FROM meeting_notes n JOIN users u ON u.id=n.author_user_id WHERE n.meeting_id=? AND n.author_user_id=?", id, who.User.ID))
	if e != nil {
		return n, e
	}
	if e = tx.Commit(); e != nil {
		return n, e
	}
	return n, nil
}
