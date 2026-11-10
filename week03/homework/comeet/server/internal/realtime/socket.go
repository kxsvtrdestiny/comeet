package realtime

import (
	"bytes"
	"comeet/server/internal/apperr"
	"context"
	"encoding/json"
	"errors"
	"github.com/coder/websocket"
	"github.com/google/uuid"
	"io"
	"net/http"
	"strings"
	"time"
)

type Event struct {
	Version   int    `json:"v"`
	Type      string `json:"type"`
	EventID   string `json:"eventId"`
	MeetingID string `json:"meetingId"`
	RequestID string `json:"requestId,omitempty"`
	Payload   any    `json:"payload"`
}
type frame struct {
	Version   int             `json:"v"`
	Type      string          `json:"type"`
	RequestID string          `json:"requestId"`
	Payload   json.RawMessage `json:"payload"`
}
type stopReason struct {
	code, message string
	terminal      bool
}
type peer struct {
	conn       *websocket.Conn
	ticket     ticket
	generation int64
	name       string
	ctx        context.Context
	cancel     context.CancelFunc
	out        chan Event
	stopping   chan stopReason
	done       chan struct{}
	chatStart  time.Time
	chatCount  int
}

func event(meetingID, kind, requestID string, payload any) Event {
	return Event{1, kind, uuid.NewString(), meetingID, requestID, payload}
}
func (p *peer) enqueue(v Event) bool {
	select {
	case <-p.ctx.Done():
		return false
	default:
	}
	select {
	case p.out <- v:
		return true
	default:
		p.cancel()
		return false
	}
}
func (p *peer) stop(code, message string, terminal bool) {
	select {
	case p.stopping <- stopReason{code, message, terminal}:
	default:
	}
}
func (p *peer) error(requestID string, err error) {
	var business *apperr.Error
	if !errors.As(err, &business) {
		business = &apperr.Error{Status: 500, Code: "INTERNAL_ERROR", Message: "操作失败，请稍后重试"}
	}
	p.enqueue(event(p.ticket.MeetingID, "error", requestID, map[string]string{"code": business.Code, "message": business.Message}))
}
func strictJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return errors.New("extra JSON")
	}
	return nil
}
func httpError(w http.ResponseWriter, err error) {
	var e *apperr.Error
	if !errors.As(err, &e) {
		e = &apperr.Error{Status: 500, Code: "INTERNAL_ERROR", Message: "服务暂不可用"}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Error-Code", e.Code)
	w.WriteHeader(e.Status)
	json.NewEncoder(w).Encode(map[string]any{"code": e.Code, "message": e.Message, "data": nil, "requestId": w.Header().Get("X-Request-ID")})
}
func (s *Service) WS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	allowed := false
	for _, v := range s.Config.AllowedOrigins {
		if origin != "" && strings.EqualFold(origin, v) {
			allowed = true
			break
		}
	}
	if !allowed {
		httpError(w, apperr.New(403, "FORBIDDEN", "WebSocket 来源不在允许列表"))
		return
	}
	query := r.URL.Query()
	if len(query) != 1 || len(query["ticket"]) != 1 {
		httpError(w, apperr.New(401, "TICKET_INVALID", "连接只允许携带一次性票据"))
		return
	}
	t, e := s.consume(r.Context(), query.Get("ticket"))
	if e != nil {
		httpError(w, e)
		return
	}
	c, e := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.Config.AllowedOrigins})
	if e != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(16 << 10)
	deadline := time.UnixMilli(t.Identity.ExpiresAt)
	if t.Identity.ExpiresAt <= 0 {
		deadline = time.Now().Add(8 * time.Hour)
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	initial, stop := context.WithTimeout(ctx, 5*time.Second)
	kind, body, e := c.Read(initial)
	stop()
	if e != nil {
		return
	}
	var first frame
	if kind != websocket.MessageText || strictJSON(body, &first) != nil || first.Version != 1 || first.Type != "room.join" || !validUUID(first.RequestID) {
		c.Close(websocket.StatusPolicyViolation, "INVALID_JOIN")
		return
	}
	var payload struct{}
	if strictJSON(first.Payload, &payload) != nil {
		c.Close(websocket.StatusPolicyViolation, "INVALID_JOIN")
		return
	}
	p := &peer{conn: c, ticket: t, ctx: ctx, cancel: cancel, out: make(chan Event, QueueSize), stopping: make(chan stopReason, 1), done: make(chan struct{})}
	defer close(p.done)
	defer s.disconnect(p)
	if e = s.attach(p, first.RequestID); e != nil {
		message := event(t.MeetingID, "error", first.RequestID, map[string]string{"code": "ALREADY_JOINED", "message": "参会会话不可用，请重新检查或返回原标签页"})
		var a *apperr.Error
		if errors.As(e, &a) {
			message.Payload = map[string]string{"code": a.Code, "message": a.Message}
		}
		data, _ := json.Marshal(message)
		write, done := context.WithTimeout(ctx, 2*time.Second)
		c.Write(write, websocket.MessageText, data)
		done()
		c.Close(websocket.StatusPolicyViolation, "JOIN_REJECTED")
		return
	}
	writerDone := make(chan struct{})
	go func() { defer close(writerDone); s.writeLoop(p) }()
	defer func() { cancel(); s.disconnect(p); c.CloseNow(); <-writerDone }()
	for {
		kind, body, e = c.Read(ctx)
		if e != nil {
			return
		}
		if kind != websocket.MessageText {
			p.error("", apperr.Invalid("仅支持 JSON 文本帧"))
			continue
		}
		var v frame
		if strictJSON(body, &v) != nil || v.Version != 1 || !validUUID(v.RequestID) {
			p.error("", apperr.Invalid("消息格式或协议版本无效"))
			continue
		}
		switch v.Type {
		case "ping":
			if e = s.checkPeer(p); e != nil {
				p.error(v.RequestID, e)
				return
			}
			p.enqueue(event(t.MeetingID, "pong", v.RequestID, map[string]int64{"serverTime": time.Now().UnixMilli()}))
		case "chat.send":
			var in ChatInput
			if strictJSON(v.Payload, &in) != nil {
				p.error(v.RequestID, apperr.Invalid("聊天字段无效"))
				continue
			}
			if e = s.chat(p, v.RequestID, in); e != nil {
				p.error(v.RequestID, e)
			}
		case "room.leave":
			var empty struct{}
			if strictJSON(v.Payload, &empty) != nil {
				p.error(v.RequestID, apperr.Invalid("离会字段无效"))
				continue
			}
			s.Leave(ctx, t.Identity, t.MeetingID, SessionInput{t.SessionID})
			return
		default:
			p.error(v.RequestID, apperr.Invalid("不支持的业务事件"))
		}
	}
}
func (s *Service) writeLoop(p *peer) {
	defer p.cancel()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	write := func(v Event) error {
		body, e := json.Marshal(v)
		if e != nil {
			return e
		}
		ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
		defer cancel()
		return p.conn.Write(ctx, websocket.MessageText, body)
	}
	finish := func(reason stopReason) {
		write(event(p.ticket.MeetingID, "error", "", map[string]string{"code": reason.code, "message": reason.message}))
		code := websocket.StatusServiceRestart
		if reason.terminal {
			code = websocket.StatusPolicyViolation
		}
		if reason.code == "LEFT_MEETING" {
			code = websocket.StatusNormalClosure
		}
		p.conn.Close(code, reason.code)
	}
	for {
		select {
		case reason := <-p.stopping:
			finish(reason)
			return
		default:
		}
		select {
		case <-p.ctx.Done():
			return
		case reason := <-p.stopping:
			finish(reason)
			return
		case v := <-p.out:
			if write(v) != nil {
				return
			}
		case <-ticker.C:
			ping, cancel := context.WithTimeout(p.ctx, 5*time.Second)
			e := p.conn.Ping(ping)
			cancel()
			if e != nil {
				return
			}
			if e = s.checkPeer(p); e != nil {
				p.error("", e)
				return
			}
		}
	}
}
func (s *Service) checkPeer(p *peer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.peers[p.ticket.SessionID] != p {
		return apperr.New(409, "SESSION_EXPIRED", "连接已失效")
	}
	v, e := s.load(p.ctx, p.ticket.SessionID)
	if e != nil {
		return e
	}
	if e = s.allowed(v, p.ticket.Identity, p.ticket.MeetingID, time.Now().UnixMilli()); e != nil {
		return e
	}
	if v.Generation != p.generation || v.State != "online" {
		return apperr.New(409, "SESSION_EXPIRED", "连接已失效")
	}
	_, e = s.DB.ExecContext(p.ctx, "UPDATE participant_sessions SET last_seen_at=? WHERE id=? AND generation=?", time.Now().UnixMilli(), v.ID, p.generation)
	return e
}
func (s *Service) disconnect(p *peer) { s.mu.Lock(); defer s.mu.Unlock(); s.disconnectLocked(p) }
func (s *Service) disconnectLocked(p *peer) {
	if s.peers[p.ticket.SessionID] != p {
		return
	}
	delete(s.peers, p.ticket.SessionID)
	p.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	v, e := s.load(ctx, p.ticket.SessionID)
	if e != nil || v.Generation != p.generation || v.State != "online" {
		return
	}
	if e = s.allowed(v, p.ticket.Identity, p.ticket.MeetingID, time.Now().UnixMilli()); e != nil {
		s.endLocked(ctx, v, "SESSION_EXPIRED", "参会会话失效")
		return
	}
	if _, e = s.DB.ExecContext(ctx, "UPDATE participant_sessions SET state='reconnecting',disconnected_at=? WHERE id=? AND generation=? AND state='online'", time.Now().UnixMilli(), v.ID, p.generation); e == nil {
		s.membersChangedLocked(v.MeetingID, "member.updated")
	}
}
