package realtime

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"time"

	"comeet/server/internal/apperr"
	"github.com/twitchtv/twirp"
)

type traceKey struct{}
type cleanupStageError struct {
	step string
	err  error
}

func (e *cleanupStageError) Error() string { return e.step }
func (e *cleanupStageError) Unwrap() error { return e.err }
func WithControlRequest(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}
func controlTrace(ctx context.Context) string { v, _ := ctx.Value(traceKey{}).(string); return v }
func logAbsent(ctx context.Context, step string) {
	slog.Info("media_cleanup_absent", "request_id", controlTrace(ctx), "step", step, "result", "already_absent")
}
func (s *Service) stepDone(ctx context.Context, id, step, identity string) bool {
	var at sql.NullInt64
	return s.DB.QueryRowContext(ctx, "SELECT completed_at FROM moderation_cleanup_steps WHERE operation_id=? AND step=? AND identity=?", id, step, identity).Scan(&at) == nil && at.Valid
}
func (s *Service) markStep(ctx context.Context, id, step, identity string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO moderation_cleanup_steps(operation_id,step,identity,completed_at) VALUES (?,?,?,?) ON CONFLICT(operation_id,step,identity) DO UPDATE SET completed_at=excluded.completed_at`, id, step, identity, time.Now().UnixMilli())
	return err
}
func (s *Service) prepareCleanup(ctx context.Context, op controlOp) (failure error) {
	defer func() {
		if failure != nil {
			var marked *cleanupStageError
			if !errors.As(failure, &marked) {
				failure = &cleanupStageError{"snapshot_database", failure}
			}
		}
	}()
	if s.stepDone(ctx, op.ID, "snapshot", "") {
		return nil
	}
	var grants int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_grants g JOIN participant_sessions p ON p.id=g.session_id WHERE g.meeting_id=? AND (?='end' OR p.user_id=?)", op.MeetingID, op.Action, op.Target).Scan(&grants); err != nil {
		return err
	}
	ids := map[string]bool{}
	if grants > 0 {
		admin, err := s.admin()
		if err != nil {
			return &cleanupStageError{"configuration", err}
		}
		call, cancel := mediaTimeout(ctx)
		active, err := admin.List(call, op.MeetingID)
		cancel()
		if err != nil {
			return &cleanupStageError{"list_participants", err}
		}
		listed := map[string]bool{}
		for _, id := range active {
			listed[id] = true
			if op.Action == "end" {
				ids[id] = true
			}
		}
		// Only live upstream identities, still-valid grants, or sessions closed by
		// this operation need removal. Expired historical grants are not replayed.
		rows, err := s.DB.QueryContext(ctx, `SELECT g.session_id,g.expires_at,p.left_at FROM media_grants g JOIN participant_sessions p ON p.id=g.session_id WHERE g.meeting_id=? AND (?='end' OR p.user_id=?)`, op.MeetingID, op.Action, op.Target)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var expiry int64
			var left sql.NullInt64
			if err = rows.Scan(&id, &expiry, &left); err != nil {
				rows.Close()
				return err
			}
			if listed[id] || expiry > op.Created || (left.Valid && left.Int64 >= op.Created) {
				ids[id] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id := range ids {
		if _, err = tx.ExecContext(ctx, "INSERT INTO moderation_cleanup_steps(operation_id,step,identity) VALUES (?,'remove',?) ON CONFLICT DO NOTHING", op.ID, id); err != nil {
			return err
		}
	}
	if grants > 0 && op.Action == "end" {
		if _, err = tx.ExecContext(ctx, "INSERT INTO moderation_cleanup_steps(operation_id,step) VALUES (?,'delete_room') ON CONFLICT DO NOTHING", op.ID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO moderation_cleanup_steps(operation_id,step,completed_at) VALUES (?,'snapshot',?) ON CONFLICT DO NOTHING", op.ID, time.Now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) runCleanup(parent, ctx context.Context, op controlOp) error {
	rows, err := s.DB.QueryContext(ctx, "SELECT step,identity FROM moderation_cleanup_steps WHERE operation_id=? AND completed_at IS NULL ORDER BY CASE step WHEN 'remove' THEN 0 ELSE 1 END,identity", op.ID)
	if err != nil {
		return s.controlFailure(parent, op, "read_checkpoints", err)
	}
	type task struct{ step, id string }
	var tasks []task
	for rows.Next() {
		var t task
		if err = rows.Scan(&t.step, &t.id); err != nil {
			rows.Close()
			return s.controlFailure(parent, op, "read_checkpoints", err)
		}
		tasks = append(tasks, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s.controlFailure(parent, op, "read_checkpoints", err)
	}
	if len(tasks) == 0 {
		return nil
	}
	admin, err := s.admin()
	if err != nil {
		return s.controlFailure(parent, op, "configuration", err)
	}
	for _, t := range tasks {
		call, cancel := mediaTimeout(ctx)
		if t.step == "remove" {
			err = admin.Remove(call, op.MeetingID, t.id, time.Now().Unix()+1)
		} else {
			err = admin.Delete(call, op.MeetingID)
		}
		cancel()
		if err != nil {
			return s.controlFailure(parent, op, t.step, err)
		}
		if err = s.markStep(ctx, op.ID, t.step, t.id); err != nil {
			return s.controlFailure(parent, op, "checkpoint", err)
		}
		slog.Info("media_cleanup_step_completed", "request_id", controlTrace(parent), "operation_id", op.ID, "step", t.step)
	}
	return nil
}
func classifyControlError(step string, err error) (string, string) {
	if step == "authorize_database" || step == "prepare_database" || step == "snapshot_database" || step == "close_sessions" || step == "checkpoint" || step == "read_checkpoints" || step == "finalize_database" {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return "DATABASE_TIMEOUT", "本次处理时间已到，已确认进度保留，请重试继续"
		}
		return "DATABASE_ERROR", "记录清理进度失败，请检查数据库可写性并凭诊断编号查看后端日志"
	}
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code, ae.Message
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "TIMEOUT", "媒体管理等待超时；已确认的清理步骤会保留，请重试继续"
	}
	var rpc twirp.Error
	if errors.As(err, &rpc) {
		switch rpc.Code() {
		case twirp.Unauthenticated, twirp.PermissionDenied:
			return "UPSTREAM_AUTH", "媒体管理鉴权失败，请核对后端 API 地址、Key 和 Secret 是否属于同一项目"
		case twirp.DeadlineExceeded, twirp.Canceled:
			return "TIMEOUT", "媒体管理超时，已完成进度保留，请重试"
		case twirp.InvalidArgument, twirp.Malformed:
			return "UPSTREAM_REQUEST", "媒体接口拒绝请求，请核对API地址和LiveKit服务版本"
		case twirp.NotFound:
			return "UPSTREAM_NOT_FOUND", "媒体目标不存在，请凭诊断编号检查接口响应"
		case twirp.Unavailable, twirp.Internal:
			return "UPSTREAM_UNAVAILABLE", "媒体管理服务暂不可用，请检查服务状态后重试"
		}
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "DNS_ERROR", "无法解析媒体管理地址，请检查后端网络和API主机名"
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "TIMEOUT", "媒体管理连接超时，请检查后端网络后重试"
		}
		return "NETWORK_ERROR", "后端无法连接媒体管理服务，请检查网络和API地址"
	}
	return "UPSTREAM_ERROR", "媒体管理返回异常，已完成进度保留，请凭诊断编号检查日志后重试"
}
func (s *Service) controlFailure(ctx context.Context, op controlOp, step string, err error) error {
	var staged *cleanupStageError
	if errors.As(err, &staged) {
		step, err = staged.step, staged.err
	}
	code, hint := classifyControlError(step, err)
	diagnostic := controlTrace(ctx)
	message := hint + "（步骤：" + step + "；诊断编号：" + diagnostic + "）"
	// Persist only allowlisted classifications and generated messages. Never store
	// upstream bodies, URLs, tokens, credentials, chat text or raw error strings.
	persist, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = s.DB.ExecContext(persist, "UPDATE moderation_operations SET last_error_code=?,last_error_step=?,last_diagnostic_id=?,last_error_message=? WHERE id=?", code, step, diagnostic, message, op.ID)
	slog.Warn("media_control_failed", "request_id", diagnostic, "operation_id", op.ID, "meeting_id", op.MeetingID, "step", step, "error_code", code)
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return apperr.New(ae.Status, ae.Code, message)
	}
	return apperr.New(502, "MEDIA_CONTROL_FAILED", message)
}
