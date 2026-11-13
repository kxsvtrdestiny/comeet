package httpapi

import (
	"comeet/server/internal/auth"
	"comeet/server/internal/meeting"
	"comeet/server/internal/realtime"
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type wsHarness struct {
	*harness
	hub    *realtime.Service
	server *httptest.Server
}

func newWSHarness(t *testing.T) *wsHarness {
	h := newHarness(t)
	h.c.Origin = "http://localhost:5173"
	h.c.AllowedOrigins = []string{h.c.Origin}
	hub := realtime.New(h.db, h.c)
	if e := hub.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	hub.Start()
	handler := Router(h.db, h.c, hub)
	h.handler = handler
	server := httptest.NewUnstartedServer(handler)
	server.Config.ReadHeaderTimeout = 5 * time.Second
	server.Config.ReadTimeout = 15 * time.Second
	server.Start()
	t.Cleanup(func() { hub.Shutdown(); server.Close() })
	return &wsHarness{h, hub, server}
}

type socketClient struct {
	conn   *websocket.Conn
	events chan realtime.Event
	done   chan struct{}
	mu     sync.Mutex
	t      *testing.T
}

func (h *wsHarness) reserve(token, id string) meeting.Reservation {
	h.t.Helper()
	s, r := h.call("POST", "/meetings/"+id+"/join", token, meeting.JoinInput{ClientRequestID: uuid.NewString()})
	check(h.t, s, r, 200, "OK")
	return unpack[meeting.Reservation](h.t, r)
}
func (h *wsHarness) ticket(token, id, session string) realtime.TicketResult {
	s, r := h.call("POST", "/meetings/"+id+"/ws-ticket", token, realtime.SessionInput{SessionID: session})
	check(h.t, s, r, 200, "OK")
	return unpack[realtime.TicketResult](h.t, r)
}
func (h *wsHarness) connect(token, id, session string) *socketClient {
	h.t.Helper()
	ticket := h.ticket(token, id, session)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(h.server.URL, "http")+"/ws?ticket="+ticket.Ticket, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{h.c.Origin}}})
	if e != nil {
		h.t.Fatal(e)
	}
	c := &socketClient{conn: conn, events: make(chan realtime.Event, 2048), done: make(chan struct{}), t: h.t}
	go func() {
		defer close(c.done)
		for {
			_, data, e := conn.Read(context.Background())
			if e != nil {
				return
			}
			var event realtime.Event
			if json.Unmarshal(data, &event) != nil {
				return
			}
			select {
			case c.events <- event:
			default:
				return
			}
		}
	}()
	h.t.Cleanup(func() { conn.CloseNow() })
	c.send("room.join", map[string]any{})
	c.wait("room.snapshot")
	return c
}
func (c *socketClient) send(kind string, payload any) string {
	c.t.Helper()
	id := uuid.NewString()
	body, _ := json.Marshal(map[string]any{"v": 1, "type": kind, "requestId": id, "payload": payload})
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := c.conn.Write(ctx, websocket.MessageText, body); e != nil {
		c.t.Fatal(e)
	}
	return id
}
func (c *socketClient) wait(kind string) realtime.Event {
	c.t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-c.events:
			if e.Type == kind {
				return e
			}
		case <-c.done:
			// A closed socket may still have multiple buffered events before its
			// terminal error. Drain them instead of failing on the first update.
			for {
				select {
				case e := <-c.events:
					if e.Type == kind {
						return e
					}
				default:
					c.t.Fatalf("socket closed waiting %s", kind)
				}
			}
		case <-timer.C:
			c.t.Fatalf("timeout waiting %s", kind)
		}
	}
}
func payload[T any](t *testing.T, e realtime.Event) T {
	t.Helper()
	b, _ := json.Marshal(e.Payload)
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
func TestRealWebSocketMembersChatHistoryAndLeave(t *testing.T) {
	h := newWSHarness(t)
	a, b := h.user("SocketHost"), h.user("SocketGuest")
	m := h.create(a.Token, "真实文字会议")
	ra, rb := h.reserve(a.Token, m.ID), h.reserve(b.Token, m.ID)
	var count int
	h.db.QueryRow("SELECT COUNT(*) FROM meeting_members").Scan(&count)
	if count != 0 {
		t.Fatal("reservation wrote attendance")
	}
	s, r := h.call("GET", "/meetings/"+m.ID+"/messages", b.Token, nil)
	check(t, s, r, 403, "FORBIDDEN")
	ca, cb := h.connect(a.Token, m.ID, ra.SessionID), h.connect(b.Token, m.ID, rb.SessionID)
	s, r = h.call("GET", "/meetings/"+m.ID+"/members", a.Token, nil)
	check(t, s, r, 200, "OK")
	members := unpack[[]realtime.Member](t, r)
	if len(members) != 2 || !members[0].IsHost || members[0].UserID != a.User.ID {
		t.Fatal("authoritative members", members)
	}
	msgID := uuid.NewString()
	ca.send("chat.send", realtime.ChatInput{ClientMessageID: msgID, Content: "A → B <script>原样文本</script>"})
	ack := payload[realtime.Message](t, ca.wait("chat.ack"))
	incoming := payload[realtime.Message](t, cb.wait("chat.created"))
	if ack != incoming || ack.SenderID != a.User.ID {
		t.Fatal("real chat mismatch")
	}
	var stored string
	if e := h.db.QueryRow("SELECT content FROM chat_messages WHERE id=?", ack.ID).Scan(&stored); e != nil || stored != ack.Content {
		t.Fatal("broadcast before persistence", e)
	}
	ca.send("chat.send", realtime.ChatInput{ClientMessageID: msgID, Content: "A → B <script>原样文本</script>"})
	if payload[realtime.Message](t, ca.wait("chat.ack")).ID != ack.ID {
		t.Fatal("duplicate retry")
	}
	cb.send("chat.send", realtime.ChatInput{ClientMessageID: uuid.NewString(), Content: "B → A"})
	cb.wait("chat.ack")
	if payload[realtime.Message](t, ca.wait("chat.created")).Content != "B → A" {
		t.Fatal("reverse chat")
	}
	h.db.QueryRow("SELECT COUNT(*) FROM chat_messages WHERE meeting_id=?", m.ID).Scan(&count)
	if count != 2 {
		t.Fatal("duplicate persisted", count)
	}
	s, r = h.call("POST", "/meetings/"+m.ID+"/leave", b.Token, realtime.SessionInput{SessionID: rb.SessionID})
	check(t, s, r, 200, "OK")
	s, r = h.call("POST", "/meetings/"+m.ID+"/leave", b.Token, realtime.SessionInput{SessionID: rb.SessionID})
	check(t, s, r, 200, "OK")
	s, r = h.call("GET", "/meetings/"+m.ID+"/members", a.Token, nil)
	check(t, s, r, 200, "OK")
	if len(unpack[[]realtime.Member](t, r)) != 1 {
		t.Fatal("ghost after leave")
	}
	var closed int
	h.db.QueryRow("SELECT COUNT(*) FROM attendance_intervals WHERE participant_session_id=? AND left_at IS NOT NULL", rb.SessionID).Scan(&closed)
	if closed != 1 {
		t.Fatal("attendance not closed once")
	}
	s, r = h.call("GET", "/meetings/"+m.ID, b.Token, nil)
	check(t, s, r, 200, "OK") // actual attendance now grants history
	ca.send("ping", map[string]any{})
	ca.wait("pong")
}
func TestTicketOriginSingleUseAndSessionBindings(t *testing.T) {
	h := newWSHarness(t)
	a, b := h.user("TicketHost"), h.user("TicketGuest")
	m := h.create(a.Token, "tickets")
	reservation := h.reserve(a.Token, m.ID)
	s, r := h.call("POST", "/meetings/"+m.ID+"/ws-ticket", b.Token, realtime.SessionInput{SessionID: reservation.SessionID})
	check(t, s, r, 403, "FORBIDDEN")
	ticket := h.ticket(a.Token, m.ID, reservation.SessionID)
	url := "ws" + strings.TrimPrefix(h.server.URL, "http") + "/ws?ticket=" + ticket.Ticket
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, response, e := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://evil.invalid"}}})
	if e == nil || response.StatusCode != 403 {
		t.Fatal("origin accepted")
	}
	conn, _, e := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{h.c.Origin}}})
	if e != nil {
		t.Fatal("bad origin consumed valid ticket", e)
	}
	defer conn.CloseNow()
	_, response, e = websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{h.c.Origin}}})
	if e == nil || response.StatusCode != 401 {
		t.Fatal("ticket reused")
	}
	// Upgrade alone is not a formal join.
	var n int
	h.db.QueryRow("SELECT COUNT(*) FROM meeting_members").Scan(&n)
	if n != 0 {
		t.Fatal("upgrade wrote attendance")
	}
}
func TestHealthyDuplicateThenResumeAndPaginatedGap(t *testing.T) {
	h := newWSHarness(t)
	a, b := h.user("ResumeHost"), h.user("ResumeGuest")
	m := h.create(a.Token, "resume")
	ra, rb := h.reserve(a.Token, m.ID), h.reserve(b.Token, m.ID)
	ca := h.connect(a.Token, m.ID, ra.SessionID)
	cb := h.connect(b.Token, m.ID, rb.SessionID)
	in := realtime.ResumeInput{SessionID: rb.SessionID, ResumeCredential: rb.ResumeCredential}
	s, r := h.call("POST", "/meetings/"+m.ID+"/resume", b.Token, in)
	check(t, s, r, 409, "ALREADY_JOINED")
	cb.conn.CloseNow()
	eventually(t, func() bool {
		var state string
		h.db.QueryRow("SELECT state FROM participant_sessions WHERE id=?", rb.SessionID).Scan(&state)
		return state == "reconnecting"
	})
	for i := 0; i < 110; i++ {
		ca.send("chat.send", realtime.ChatInput{ClientMessageID: uuid.NewString(), Content: strings.Repeat("字", 1) + "-" + fmt.Sprint(i)})
		ca.wait("chat.ack")
	}
	s, r = h.call("POST", "/meetings/"+m.ID+"/resume", b.Token, in)
	check(t, s, r, 200, "OK")
	h.connect(b.Token, m.ID, rb.SessionID)
	cursor := int64(0)
	total := 0
	for {
		s, r = h.call("GET", fmt.Sprintf("/meetings/%s/messages?afterId=%d&limit=100", m.ID, cursor), b.Token, nil)
		check(t, s, r, 200, "OK")
		page := unpack[realtime.MessagePage](t, r)
		for _, message := range page.Items {
			if message.ID <= cursor {
				t.Fatal("unordered/double gap")
			}
			cursor = message.ID
		}
		total += len(page.Items)
		if !page.HasMore {
			break
		}
	}
	if total != 110 {
		t.Fatal("gap not fully paginated", total)
	}
	s, r = h.call("GET", "/meetings/"+m.ID+"/messages?limit=50", b.Token, nil)
	check(t, s, r, 200, "OK")
	page := unpack[realtime.MessagePage](t, r)
	if len(page.Items) != 50 || !page.HasMore {
		t.Fatal("history page")
	}
	s, r = h.call("GET", fmt.Sprintf("/meetings/%s/messages?beforeId=%d&limit=100", m.ID, page.NextCursor), b.Token, nil)
	check(t, s, r, 200, "OK")
	if len(unpack[realtime.MessagePage](t, r).Items) != 60 {
		t.Fatal("older history")
	}
	var intervals int
	h.db.QueryRow("SELECT COUNT(*) FROM attendance_intervals WHERE participant_session_id=?", rb.SessionID).Scan(&intervals)
	if intervals != 1 {
		t.Fatal("reconnect duplicated attendance")
	}
	s, r = h.call("GET", "/meetings/"+m.ID+"/messages?beforeId=1&afterId=0", b.Token, nil)
	check(t, s, r, 400, "VALIDATION_ERROR")
}
func TestSocketExpiresAtJWTAndLogoutClosesMembers(t *testing.T) {
	h := newWSHarness(t)
	a := h.user("ExpirySocket")
	m := h.create(a.Token, "expiry")
	reservation := h.reserve(a.Token, m.ID)
	claims := &auth.Claims{}
	jwt.ParseWithClaims(a.Token, claims, func(*jwt.Token) (any, error) { return []byte(h.c.JWTSecret), nil })
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(2 * time.Second))
	short, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(h.c.JWTSecret))
	c := h.connect(short, m.ID, reservation.SessionID)
	select {
	case <-c.done:
	case <-time.After(4 * time.Second):
		t.Fatal("socket lived beyond JWT expiry")
	}
	eventually(t, func() bool {
		var state string
		h.db.QueryRow("SELECT state FROM participant_sessions WHERE id=?", reservation.SessionID).Scan(&state)
		return state == "left"
	})
	reservation = h.reserve(a.Token, m.ID)
	c = h.connect(a.Token, m.ID, reservation.SessionID)
	s, r := h.call("POST", "/auth/logout", a.Token, nil)
	check(t, s, r, 200, "OK")
	select {
	case <-c.done:
	case <-time.After(4 * time.Second):
		t.Fatal("logout kept socket alive")
	}
}

func TestHeartbeatKeepsHealthySocketBeyondHTTPReadTimeout(t *testing.T) {
	h := newWSHarness(t)
	a := h.user("HeartbeatUser")
	m := h.create(a.Token, "heartbeat")
	r := h.reserve(a.Token, m.ID)
	c := h.connect(a.Token, m.ID, r.SessionID)
	var before, after int64
	h.db.QueryRow("SELECT last_seen_at FROM participant_sessions WHERE id=?", r.SessionID).Scan(&before)
	time.Sleep(17 * time.Second) // real protocol Ping/Pong, no fake clock or JS timer
	c.send("ping", map[string]any{})
	c.wait("pong")
	h.db.QueryRow("SELECT last_seen_at FROM participant_sessions WHERE id=?", r.SessionID).Scan(&after)
	if after <= before {
		t.Fatal("heartbeat did not refresh liveness")
	}
}
func TestMalformedChatAndObjectPermissions(t *testing.T) {
	h := newWSHarness(t)
	a, b := h.user("SafeHost"), h.user("SafeGuest")
	m := h.create(a.Token, "safe")
	r := h.reserve(a.Token, m.ID)
	c := h.connect(a.Token, m.ID, r.SessionID)
	for _, raw := range []string{`{`, `{"v":77,"type":"chat.send","requestId":"bad","payload":{}}`} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		e := c.conn.Write(ctx, websocket.MessageText, []byte(raw))
		cancel()
		if e != nil {
			t.Fatal(e)
		}
		c.wait("error")
	}
	for _, in := range []any{realtime.ChatInput{ClientMessageID: uuid.NewString(), Content: " "}, realtime.ChatInput{ClientMessageID: uuid.NewString(), Content: strings.Repeat("字", 2001)}, map[string]any{"clientMessageId": uuid.NewString(), "content": "spoof", "senderId": b.User.ID}} {
		c.send("chat.send", in)
		c.wait("error")
	}
	s, out := h.call("GET", "/meetings/"+m.ID+"/messages", b.Token, nil)
	check(t, s, out, 403, "FORBIDDEN")
	s, out = h.call("POST", "/meetings/"+m.ID+"/leave", b.Token, realtime.SessionInput{SessionID: r.SessionID})
	check(t, s, out, 403, "FORBIDDEN")
	c.send("chat.send", realtime.ChatInput{ClientMessageID: uuid.NewString(), Content: "still working"})
	c.wait("chat.ack")
}
