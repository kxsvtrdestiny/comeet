package realtime

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"comeet/server/internal/apperr"
	"comeet/server/internal/auth"
	"github.com/google/uuid"
)

type ControlInput struct {
	RequestID string `json:"requestId"`
}
type ControlResult struct {
	OperationID string `json:"operationId"`
	State       string `json:"state"`
}
type controlOp struct {
	ID, MeetingID, Actor, Action, Target, State string
	Created                                     int64
}

// A separate gate serializes management actions. No DB transaction or realtime
// mutex is held during upstream network calls; chat/heartbeats remain usable.
func (s *Service) Control(ctx context.Context, who auth.Identity, meetingID, action, target string, in ControlInput) (ControlResult, error) {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if action != "mute" && action != "remove" && action != "end" {
		return ControlResult{}, apperr.Invalid("会控操作无效")
	}
	if !validUUID(in.RequestID) {
		return ControlResult{}, apperr.Invalid("requestId 必须为 UUID")
	}
	var host, status string
	if err := s.DB.QueryRowContext(ctx, "SELECT host_user_id,status FROM meetings WHERE id=?", meetingID).Scan(&host, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ControlResult{}, apperr.New(404, "NOT_FOUND", "会议不存在")
		}
		return ControlResult{}, err
	}
	if host != who.User.ID {
		return ControlResult{}, apperr.New(403, "HOST_REQUIRED", "仅主持人可执行此操作")
	}
	if action != "end" && (target == host || !validUUID(target)) {
		return ControlResult{}, apperr.Invalid("请选择其他参会成员")
	}
	op, err := s.prepareControl(ctx, who.User.ID, meetingID, action, target, status, in.RequestID)
	if err != nil {
		return ControlResult{}, err
	}
	if op.State == "completed" {
		return ControlResult{op.ID, op.State}, nil
	}
	if err = s.applyControl(ctx, op); err != nil {
		return ControlResult{}, err
	}
	return ControlResult{op.ID, "completed"}, nil
}
func scanControl(row interface{ Scan(...any) error }) (o controlOp, e error) {
	e = row.Scan(&o.ID, &o.MeetingID, &o.Actor, &o.Action, &o.Target, &o.State, &o.Created)
	return
}

const controlColumns = "id,meeting_id,actor_id,action,target_user_id,state,created_at"

func (s *Service) prepareControl(ctx context.Context, actor, meetingID, action, target, status, requestID string) (controlOp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return controlOp{}, apperr.New(503, "SERVER_RESTART", "服务正在重启，请稍后重试")
	}
	old, err := scanControl(s.DB.QueryRowContext(ctx, "SELECT "+controlColumns+" FROM moderation_operations WHERE id=?", requestID))
	if err == nil {
		if old.MeetingID != meetingID || old.Actor != actor || old.Action != action || old.Target != target {
			return old, apperr.New(409, "REQUEST_CONFLICT", "操作标识已用于其他请求")
		}
		return old, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return old, err
	}
	if action != "mute" {
		old, err = scanControl(s.DB.QueryRowContext(ctx, "SELECT "+controlColumns+" FROM moderation_operations WHERE meeting_id=? AND action=? AND target_user_id=?", meetingID, action, target))
		if err == nil {
			return old, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return old, err
		}
	}
	if status != "active" {
		return old, apperr.New(409, "MEETING_ENDED", "会议已结束或正在结束")
	}
	if action != "end" {
		var n int
		if err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM participant_sessions WHERE meeting_id=? AND user_id=? AND state IN ('online','reconnecting')", meetingID, target).Scan(&n); err != nil {
			return old, err
		}
		if n == 0 {
			return old, apperr.New(409, "MEMBER_NOT_PRESENT", "该成员已离会")
		}
	}
	now := time.Now().UnixMilli()
	op := controlOp{requestID, meetingID, actor, action, target, "pending", now}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return op, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO moderation_operations("+controlColumns+") VALUES (?,?,?,?,?,?,?)", op.ID, op.MeetingID, op.Actor, op.Action, op.Target, op.State, op.Created); err != nil {
		return op, err
	}
	if action == "remove" {
		_, err = tx.ExecContext(ctx, "INSERT INTO meeting_bans(meeting_id,user_id,created_by,created_at) VALUES (?,?,?,?) ON CONFLICT DO NOTHING", meetingID, target, actor, now)
	} else if action == "end" {
		_, err = tx.ExecContext(ctx, "UPDATE meetings SET status='ending' WHERE id=? AND status='active'", meetingID)
	}
	if err != nil {
		return op, err
	}
	if err = tx.Commit(); err != nil {
		return op, err
	}
	return op, nil
}
func (s *Service) closeControlSessions(ctx context.Context, op controlOp) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	query := "SELECT id FROM participant_sessions WHERE meeting_id=? AND state!='left'"
	args := []any{op.MeetingID}
	if op.Action == "remove" {
		query += " AND user_id=?"
		args = append(args, op.Target)
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	code, msg := "REMOVED_FROM_MEETING", "你已被主持人移出本场会议"
	if op.Action == "end" {
		code, msg = "MEETING_ENDING", "会议正在结束"
	}
	for _, id := range ids {
		v, e := s.load(ctx, id)
		if e != nil {
			return e
		}
		if e = s.endLocked(ctx, v, code, msg); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) applyControl(ctx context.Context, op controlOp) error {
	if op.Action != "mute" {
		if err := s.closeControlSessions(ctx, op); err != nil {
			return err
		}
	}
	// Only identities with an issued token can have an upstream media presence.
	query := `SELECT g.session_id FROM media_grants g JOIN participant_sessions p ON p.id=g.session_id WHERE g.meeting_id=?`
	args := []any{op.MeetingID}
	if op.Action != "end" {
		query += " AND p.user_id=?"
		args = append(args, op.Target)
	}
	if op.Action == "mute" {
		query += " AND p.state='online'"
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	var identities []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		identities = append(identities, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if op.Action == "mute" && len(identities) == 0 {
		return apperr.New(409, "MICROPHONE_NOT_PUBLISHED", "该成员当前未开麦")
	}
	if len(identities) > 0 {
		admin, e := s.admin()
		if e != nil {
			return e
		}
		callctx, cancel := mediaTimeout(ctx)
		defer cancel()
		for _, id := range identities {
			if op.Action == "mute" {
				err = admin.Mute(callctx, op.MeetingID, id)
			} else {
				err = admin.Remove(callctx, op.MeetingID, id, time.Now().Unix()+1)
			}
			if err != nil {
				var ae *apperr.Error
				if errors.As(err, &ae) {
					return ae
				}
				return apperr.New(502, "MEDIA_CONTROL_FAILED", "媒体管理未完全完成，可重试；移出封禁或会议结束中状态仍保留")
			}
		}
		if op.Action == "end" {
			if err = admin.Delete(callctx, op.MeetingID); err != nil {
				return apperr.New(502, "MEDIA_CONTROL_FAILED", "媒体房间清理失败，会议仍在结束中，请重试")
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if op.Action == "end" {
		if _, err = tx.ExecContext(ctx, "UPDATE meetings SET status='ended',ended_at=COALESCE(ended_at,?) WHERE id=?", time.Now().UnixMilli(), op.MeetingID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE moderation_operations SET state='completed' WHERE id=?", op.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if op.Action == "mute" {
		for _, p := range s.peers {
			if p.ticket.MeetingID == op.MeetingID && p.ticket.Identity.User.ID == op.Target {
				p.enqueue(event(op.MeetingID, "member.muted", "", map[string]string{"message": "主持人已将你静音，你可以自行开麦"}))
			}
		}
	}
	return nil
}

// Run once at startup, not a general job queue. Durable pending end/remove
// operations remain available for explicit host retries if the provider is down.
func (s *Service) RetryControls(ctx context.Context) error {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	ending, err := s.DB.QueryContext(ctx, "SELECT m.id,m.host_user_id FROM meetings m WHERE m.status='ending' AND NOT EXISTS (SELECT 1 FROM moderation_operations o WHERE o.meeting_id=m.id AND o.action='end')")
	if err != nil {
		return err
	}
	type unfinished struct{ id, host string }
	var recoverable []unfinished
	for ending.Next() {
		var v unfinished
		if err = ending.Scan(&v.id, &v.host); err != nil {
			ending.Close()
			return err
		}
		recoverable = append(recoverable, v)
	}
	err = ending.Err()
	ending.Close()
	if err != nil {
		return err
	}
	for _, v := range recoverable {
		if _, err = s.DB.ExecContext(ctx, "INSERT INTO moderation_operations("+controlColumns+") VALUES (?,?,?,'end','','pending',?)", uuid.NewString(), v.id, v.host, time.Now().UnixMilli()); err != nil {
			return err
		}
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT "+controlColumns+" FROM moderation_operations WHERE state='pending' AND action IN ('end','remove') ORDER BY created_at")
	if err != nil {
		return err
	}
	var ops []controlOp
	for rows.Next() {
		op, e := scanControl(rows)
		if e != nil {
			rows.Close()
			return e
		}
		ops = append(ops, op)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var first error
	for _, op := range ops {
		if e := s.applyControl(ctx, op); e != nil && first == nil {
			first = e
		}
	}
	return first
}

// The host can recover pending cleanup after refresh or personal leave.
func (s *Service) PendingControls(ctx context.Context, who auth.Identity, meetingID string) ([]map[string]string, error) {
	var host string
	if err := s.DB.QueryRowContext(ctx, "SELECT host_user_id FROM meetings WHERE id=?", meetingID).Scan(&host); err != nil {
		return nil, apperr.New(404, "NOT_FOUND", "会议不存在")
	}
	if host != who.User.ID {
		return nil, apperr.New(403, "HOST_REQUIRED", "仅主持人可查看会控操作")
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT id,action,target_user_id FROM moderation_operations WHERE meeting_id=? AND state='pending' ORDER BY created_at", meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []map[string]string{}
	for rows.Next() {
		var id, action, target string
		if err = rows.Scan(&id, &action, &target); err != nil {
			return nil, err
		}
		list = append(list, map[string]string{"id": id, "action": action, "targetUserId": target})
	}
	return list, rows.Err()
}
