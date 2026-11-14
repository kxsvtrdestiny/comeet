package summary

import (
	"comeet/server/internal/apperr"
	"comeet/server/internal/auth"
	"comeet/server/internal/config"
	"comeet/server/internal/meeting"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const Disclaimer = "由 DeepSeek 基于聊天与手动笔记生成，不含语音转写，请核对内容。"

type Service struct {
	DB     *sql.DB
	Config config.Config
	Client *http.Client
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	slots  chan struct{}
	closed bool
}

func New(db *sql.DB, c config.Config) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{DB: db, Config: c, Client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, ctx: ctx, cancel: cancel, slots: make(chan struct{}, 2)}
}
func (s *Service) maxChars() int {
	if s.Config.AIMaxSourceChars > 0 {
		return s.Config.AIMaxSourceChars
	}
	return 20000
}
func (s *Service) maxBytes() int {
	if s.Config.AIMaxSourceBytes > 0 {
		return s.Config.AIMaxSourceBytes
	}
	return 80000
}
func (s *Service) Recover(ctx context.Context) error {
	_, e := s.DB.ExecContext(ctx, "UPDATE summary_jobs SET status='failed',result_json='',error_code='SERVER_RESTART',error_message='服务已重启，生成结果未确认；请手动重试，可能再次产生费用',finished_at=? WHERE status='running'", time.Now().UnixMilli())
	return e
}
func (s *Service) Shutdown() { s.mu.Lock(); s.closed = true; s.cancel(); s.mu.Unlock(); s.wg.Wait() }

type GenerateInput struct {
	RequestID  string `json:"requestId"`
	Consent    bool   `json:"consent"`
	Regenerate bool   `json:"regenerate"`
}
type Job struct {
	ID           string  `json:"id"`
	Status       string  `json:"status"`
	SourceHash   string  `json:"sourceHash"`
	Model        string  `json:"model"`
	Result       *Result `json:"result,omitempty"`
	ErrorCode    string  `json:"errorCode,omitempty"`
	ErrorMessage string  `json:"errorMessage,omitempty"`
	DiagnosticID string  `json:"diagnosticId,omitempty"`
	CreatedAt    int64   `json:"createdAt"`
	FinishedAt   *int64  `json:"finishedAt"`
	MessageCount int     `json:"messageCount"`
	NoteCount    int     `json:"noteCount"`
}

const jobColumns = "id,status,source_hash,model,result_json,error_code,error_message,diagnostic_id,created_at,finished_at,source_snapshot"

func scanJob(row interface{ Scan(...any) error }) (j Job, e error) {
	var result, snapshot string
	e = row.Scan(&j.ID, &j.Status, &j.SourceHash, &j.Model, &result, &j.ErrorCode, &j.ErrorMessage, &j.DiagnosticID, &j.CreatedAt, &j.FinishedAt, &snapshot)
	if e != nil {
		return
	}
	if result != "" {
		j.Result = &Result{}
		if e = json.Unmarshal([]byte(result), j.Result); e != nil {
			return
		}
	}
	var snap Snapshot
	if e = json.Unmarshal([]byte(snapshot), &snap); e != nil {
		return
	}
	for _, v := range snap.Entries {
		if len(v.ID) > 8 && v.ID[:8] == "message:" {
			j.MessageCount++
		} else {
			j.NoteCount++
		}
	}
	return
}

type View struct {
	Configured  bool   `json:"configured"`
	CanGenerate bool   `json:"canGenerate"`
	Reason      string `json:"reason"`
	Model       string `json:"model"`
	SourceHash  string `json:"sourceHash"`
	SourceError string `json:"sourceError,omitempty"`
	MaxChars    int    `json:"maxChars"`
	MaxBytes    int    `json:"maxBytes"`
	Disclaimer  string `json:"disclaimer"`
	Jobs        []Job  `json:"jobs"`
}

func (s *Service) authorize(ctx context.Context, who auth.Identity, mid string) (meeting.Meeting, error) {
	return (&meeting.Service{DB: s.DB, Config: s.Config}).Detail(ctx, who, mid)
}
func (s *Service) expire(ctx context.Context) error {
	seconds := s.Config.AITimeout
	if seconds <= 0 {
		seconds = 90
	}
	_, e := s.DB.ExecContext(ctx, "UPDATE summary_jobs SET status='failed',result_json='',error_code='AI_RESULT_UNCONFIRMED',error_message='生成超时或结果未能保存，未确认成功；请检查服务并手动重试，可能再次产生费用',finished_at=? WHERE status='running' AND created_at<?", time.Now().UnixMilli(), time.Now().Add(-time.Duration(seconds+15)*time.Second).UnixMilli())
	return e
}
func (s *Service) View(ctx context.Context, who auth.Identity, mid string) (View, error) {
	v := View{Configured: s.Config.AIConfigured(), Model: s.Config.AIModel, MaxChars: s.maxChars(), MaxBytes: s.maxBytes(), Disclaimer: Disclaimer, Jobs: []Job{}}
	m, e := s.authorize(ctx, who, mid)
	if e != nil {
		return v, e
	}
	if e = s.expire(ctx); e != nil {
		return v, e
	}
	v.CanGenerate = who.User.ID == m.HostUserID && m.Status == "ended" && v.Configured
	switch {
	case !v.Configured:
		v.Reason = "DeepSeek未配置，请在后端server/.env启用并填写模型及Key后重启；不会生成示例纪要"
	case m.Status != "ended":
		v.Reason = "会议完全结束后才可生成纪要"
	case who.User.ID != m.HostUserID:
		v.Reason = "仅主持人可生成；实际参会者可查看和导出"
	}
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return v, e
	}
	defer tx.Rollback()
	snap, _, hash, e := s.snapshot(ctx, tx, mid)
	if e != nil {
		var ae *apperr.Error
		if errors.As(e, &ae) {
			v.SourceError = ae.Code
			v.Reason = ae.Message
			v.CanGenerate = false
		} else {
			return v, e
		}
	} else {
		v.SourceHash = hash
		if len(snap.Entries) == 0 {
			v.SourceError = "SOURCE_EMPTY"
			v.CanGenerate = false
			v.Reason = "本场没有有效聊天或已保存手动笔记，记录不足，不调用模型"
		}
	}
	rows, e := tx.QueryContext(ctx, "SELECT "+jobColumns+" FROM summary_jobs WHERE meeting_id=? AND (id IN (SELECT id FROM summary_jobs WHERE meeting_id=? ORDER BY created_at DESC,id DESC LIMIT 10) OR id=(SELECT id FROM summary_jobs WHERE meeting_id=? AND status='succeeded' ORDER BY created_at DESC,id DESC LIMIT 1)) ORDER BY created_at DESC,id DESC", mid, mid, mid)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		j, e := scanJob(rows)
		if e != nil {
			rows.Close()
			return v, e
		}
		v.Jobs = append(v.Jobs, j)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	return v, tx.Commit()
}
func (s *Service) Generate(ctx context.Context, who auth.Identity, mid string, in GenerateInput, trace string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, e := s.authorize(ctx, who, mid)
	if e != nil {
		return Job{}, e
	}
	if who.User.ID != m.HostUserID {
		return Job{}, apperr.New(403, "HOST_REQUIRED", "仅主持人可生成纪要")
	}
	if _, e = uuid.Parse(in.RequestID); e != nil || !in.Consent {
		return Job{}, apperr.Invalid("必须提供UUID操作编号并确认将聊天与笔记发送至DeepSeek")
	}
	if e = s.expire(ctx); e != nil {
		return Job{}, e
	}
	// A request whose response was lost must return its durable task, even if notes changed.
	old, e := scanJob(s.DB.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM summary_jobs WHERE id=(SELECT job_id FROM summary_requests WHERE request_id=? AND meeting_id=?)", in.RequestID, mid))
	if e == nil {
		return old, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return Job{}, e
	}
	if s.closed {
		return Job{}, apperr.New(503, "SERVER_RESTART", "服务正在退出，请稍后重试")
	}
	if m.Status != "ended" {
		return Job{}, apperr.New(409, "MEETING_NOT_ENDED", "会议完全结束后才可生成纪要")
	}
	if !s.Config.AIConfigured() {
		return Job{}, apperr.New(503, "AI_NOT_CONFIGURED", "DeepSeek未配置；请在后端.env设置启用、模型和Key后重启，不会生成假纪要")
	}
	old, e = scanJob(s.DB.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM summary_jobs WHERE meeting_id=? AND status='running'", mid))
	if e == nil {
		_, e = s.DB.ExecContext(ctx, "INSERT INTO summary_requests VALUES (?,?,?)", in.RequestID, mid, old.ID)
		return old, aliasError(e)
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return Job{}, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Job{}, e
	}
	defer tx.Rollback()
	snap, raw, hash, e := s.snapshot(ctx, tx, mid)
	if e != nil {
		return Job{}, e
	}
	if len(snap.Entries) == 0 {
		return Job{}, apperr.New(422, "SOURCE_EMPTY", "本场没有有效聊天或已保存笔记，不调用模型")
	}
	if !in.Regenerate {
		old, e = scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM summary_jobs WHERE meeting_id=? AND source_hash=? AND status='succeeded' ORDER BY created_at DESC LIMIT 1", mid, hash))
		if e == nil {
			if _, e = tx.ExecContext(ctx, "INSERT INTO summary_requests VALUES (?,?,?)", in.RequestID, mid, old.ID); e != nil {
				return Job{}, aliasError(e)
			}
			return old, tx.Commit()
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return Job{}, e
		}
	}
	var count int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM summary_jobs WHERE meeting_id=? AND created_at>?", mid, time.Now().Add(-time.Minute).UnixMilli()).Scan(&count); e != nil {
		return Job{}, e
	}
	if count >= 3 {
		return Job{}, apperr.New(429, "AI_RATE_LIMITED", "本场每分钟最多3次新生成，请稍后重试；重复查询不会重复计费")
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return Job{}, apperr.New(429, "AI_BUSY", "生成任务已满，请稍后重试")
	}
	reserved := true
	defer func() {
		if reserved {
			<-s.slots
		}
	}()
	if trace == "" {
		trace = uuid.NewString()
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO summary_jobs(id,meeting_id,requested_by,status,source_hash,source_snapshot,model,diagnostic_id,created_at) VALUES (?,?,?,'running',?,?,?,?,?)", in.RequestID, mid, who.User.ID, hash, raw, s.Config.AIModel, trace, time.Now().UnixMilli())
	if e != nil {
		return Job{}, aliasError(e)
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO summary_requests VALUES (?,?,?)", in.RequestID, mid, in.RequestID); e != nil {
		return Job{}, aliasError(e)
	}
	j, e := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM summary_jobs WHERE id=?", in.RequestID))
	if e != nil {
		return Job{}, e
	}
	if e = tx.Commit(); e != nil {
		return Job{}, e
	}
	s.wg.Add(1)
	reserved = false
	go func() { defer s.wg.Done(); defer func() { <-s.slots }(); s.run(j, snap, raw) }()
	return j, nil
}
func (s *Service) run(j Job, snap Snapshot, raw string) {
	seconds := s.Config.AITimeout
	if seconds <= 0 {
		seconds = 90
	}
	ctx, cancel := context.WithTimeout(s.ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, validationTraceKey{}, validationTrace{j.ID, j.DiagnosticID})
	result, e := s.complete(ctx, snap, raw)
	status, code, message, resultJSON := "succeeded", "", "", ""
	if e != nil {
		status = "failed"
		var ae *apperr.Error
		if errors.As(e, &ae) {
			code, message = ae.Code, ae.Message
		} else {
			code, message = "AI_UNAVAILABLE", "DeepSeek服务暂不可用，请稍后手动重试，可能再次产生费用"
		}
	} else {
		b, _ := json.Marshal(result)
		resultJSON = string(b)
	}
	if s.ctx.Err() != nil {
		status, code, message, resultJSON = "failed", "SERVER_RESTART", "服务退出中断生成，结果未确认；可手动重试，可能再次产生费用", ""
	}
	persist, cancelDB := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelDB()
	stored, err := s.DB.ExecContext(persist, "UPDATE summary_jobs SET status=?,result_json=?,error_code=?,error_message=?,finished_at=? WHERE id=? AND status='running'", status, resultJSON, code, message, time.Now().UnixMilli(), j.ID)
	if err != nil {
		slog.Error("summary_persist_failed", "task_id", j.ID, "request_id", j.DiagnosticID, "error_code", "DATABASE_ERROR")
		return
	}
	if n, e := stored.RowsAffected(); e != nil || n != 1 {
		slog.Warn("summary_result_not_committed", "task_id", j.ID, "request_id", j.DiagnosticID, "error_code", "RESULT_UNCONFIRMED")
		return
	}
	slog.Info("summary_task_finished", "task_id", j.ID, "request_id", j.DiagnosticID, "status", status, "error_code", code)
}

func aliasError(e error) error {
	if apperr.IsUnique(e) {
		return apperr.New(409, "REQUEST_CONFLICT", "操作编号已用于另一场会议，请刷新后重新发起")
	}
	return e
}
