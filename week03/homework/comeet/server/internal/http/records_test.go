package httpapi

import (
	"comeet/server/internal/meeting"
	"comeet/server/internal/realtime"
	"comeet/server/internal/store"
	"sync"
	"testing"
	"time"
)

func TestRecordsNotesRealAttendancePermissionsAndConflict(t *testing.T) {
	h := newWSHarness(t)
	a, b, c := h.user("NoteHost"), h.user("NoteGuest"), h.user("NoteOutsider")
	m := h.create(a.Token, "历史权限与笔记")
	ra, rb := h.reserve(a.Token, m.ID), h.reserve(b.Token, m.ID)
	h.connect(a.Token, m.ID, ra.SessionID)
	formatted := m.MeetingNo[:3] + "-" + m.MeetingNo[3:6] + "-" + m.MeetingNo[6:]
	code, listReply := h.call("GET", "/meetings?keyword="+formatted, a.Token, nil)
	check(t, code, listReply, 200, "OK")
	if list := unpack[meeting.ListResult](t, listReply); len(list.Items) != 1 || list.Items[0].ID != m.ID {
		t.Fatal("formatted meeting number search failed", list)
	}
	record := "/meetings/" + m.ID + "/record"
	notes := "/meetings/" + m.ID + "/notes"
	for _, path := range []string{record, notes} {
		s, r := h.call("GET", path, b.Token, nil)
		check(t, s, r, 403, "FORBIDDEN")
		s, r = h.call("GET", path, c.Token, nil)
		check(t, s, r, 403, "FORBIDDEN")
	}
	cb := h.connect(b.Token, m.ID, rb.SessionID)
	start := time.Now().Add(-time.Minute).UnixMilli()
	h.db.Exec("UPDATE attendance_intervals SET joined_at=? WHERE participant_session_id=?", start, rb.SessionID)
	s, r := h.call("GET", record, a.Token, nil)
	check(t, s, r, 200, "OK")
	rec := unpack[meeting.Record](t, r)
	if len(rec.Attendees) != 2 {
		t.Fatal("incorrect attendee count", rec)
	}
	for _, v := range rec.Attendees {
		if v.UserID == b.User.ID && (v.DurationMillis < 59000 || len(v.Intervals) != 1) {
			t.Fatal("duration missing", v)
		}
	}
	s, r = h.call("PUT", notes+"/me", a.Token, meeting.NoteInput{Content: "真实手动笔记 <script>文本</script>", Version: 0})
	check(t, s, r, 200, "OK")
	first := unpack[meeting.Note](t, r)
	if first.AuthorID != a.User.ID || first.Version != 1 {
		t.Fatal(first)
	}
	s, r = h.call("PUT", notes+"/me", b.Token, map[string]any{"content": "冒充作者", "version": 1, "authorId": a.User.ID})
	check(t, s, r, 400, "VALIDATION_ERROR")
	s, r = h.call("PUT", notes+"/me", c.Token, meeting.NoteInput{Content: "越权", Version: 0})
	check(t, s, r, 403, "FORBIDDEN")
	s, r = h.call("PUT", notes+"/me", b.Token, meeting.NoteInput{Content: "B自己的笔记", Version: 0})
	check(t, s, r, 200, "OK")
	var wg sync.WaitGroup
	success, conflicts := 0, 0
	var mu sync.Mutex
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, reply := h.call("PUT", notes+"/me", a.Token, meeting.NoteInput{Content: "并发编辑", Version: 1})
			mu.Lock()
			defer mu.Unlock()
			if code == 200 {
				success++
			} else if code == 409 && reply.Code == "NOTE_VERSION_CONFLICT" {
				conflicts++
			} else {
				t.Errorf("unexpected %d %s", code, reply.Code)
			}
		}()
	}
	wg.Wait()
	if success != 1 || conflicts != 1 {
		t.Fatal("lost version conflict", success, conflicts)
	}
	s, r = h.call("GET", notes, b.Token, nil)
	check(t, s, r, 200, "OK")
	all := unpack[[]meeting.Note](t, r)
	if len(all) != 2 {
		t.Fatal(all)
	}
	h.call("POST", "/meetings/"+m.ID+"/leave", b.Token, realtime.SessionInput{SessionID: rb.SessionID})
	s, r = h.call("GET", record, b.Token, nil)
	check(t, s, r, 200, "OK")
	after := unpack[meeting.Record](t, r)
	for _, v := range after.Attendees {
		if v.UserID == b.User.ID && v.Intervals[0].LeftAt == nil {
			t.Fatal("unclosed interval")
		}
	}
	_ = cb
}

func TestNotesSurviveDatabaseReopen(t *testing.T) {
	h := newHarness(t)
	a := h.user("PersistentNoteHost")
	m := h.create(a.Token, "持久笔记")
	path := "/meetings/" + m.ID + "/notes"
	s, r := h.call("PUT", path+"/me", a.Token, meeting.NoteInput{Content: "保存后重启恢复", Version: 0})
	check(t, s, r, 200, "OK")
	h.db.Close()
	db, e := store.Open(h.path)
	if e != nil {
		t.Fatal(e)
	}
	h.db = db
	h.handler = Router(db, h.c)
	s, r = h.call("GET", path, a.Token, nil)
	check(t, s, r, 200, "OK")
	list := unpack[[]meeting.Note](t, r)
	if len(list) != 1 || list[0].Content != "保存后重启恢复" || list[0].Version != 1 {
		t.Fatal(list)
	}
	s, r = h.call("PUT", path+"/me", a.Token, meeting.NoteInput{Content: "刷新后继续保存", Version: 1})
	check(t, s, r, 200, "OK")
}

func TestRecordMultipleAttendanceIntervalsDoNotCountReservation(t *testing.T) {
	h := newWSHarness(t)
	a := h.user("IntervalHost")
	m := h.create(a.Token, "多次实际进出")
	r := h.reserve(a.Token, m.ID)
	h.connect(a.Token, m.ID, r.SessionID)
	now := time.Now().UnixMilli()
	h.db.Exec("UPDATE attendance_intervals SET joined_at=? WHERE participant_session_id=?", now-10000, r.SessionID)
	s, reply := h.call("POST", "/meetings/"+m.ID+"/leave", a.Token, realtime.SessionInput{SessionID: r.SessionID})
	check(t, s, reply, 200, "OK")
	r2 := h.reserve(a.Token, m.ID)
	h.connect(a.Token, m.ID, r2.SessionID)
	h.db.Exec("UPDATE attendance_intervals SET joined_at=? WHERE participant_session_id=?", now-5000, r2.SessionID)
	s, reply = h.call("POST", "/meetings/"+m.ID+"/leave", a.Token, realtime.SessionInput{SessionID: r2.SessionID})
	check(t, s, reply, 200, "OK")
	h.reserve(a.Token, m.ID) // a new reservation must not become a third attendance interval
	s, reply = h.call("GET", "/meetings/"+m.ID+"/record", a.Token, nil)
	check(t, s, reply, 200, "OK")
	record := unpack[meeting.Record](t, reply)
	if len(record.Attendees) != 1 || len(record.Attendees[0].Intervals) != 2 || record.Attendees[0].DurationMillis < 15000 {
		t.Fatal(record)
	}
}
