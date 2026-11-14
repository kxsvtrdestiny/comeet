package summary

import (
	"comeet/server/internal/apperr"
	"comeet/server/internal/auth"
	"comeet/server/internal/config"
	"comeet/server/internal/meeting"
	"comeet/server/internal/store"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fixture struct {
	s                *Service
	host, guest, out auth.Identity
	mid, nid, path   string
}

func setup(t *testing.T, h http.HandlerFunc) *fixture {
	t.Helper()
	up := httptest.NewServer(h)
	t.Cleanup(up.Close)
	path := filepath.Join(t.TempDir(), "summary.db")
	db, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	c := config.Config{Env: "development", AIEnabled: true, AIBase: up.URL, AIKey: "test-private-key", AIModel: "fixture-model", AITimeout: 1, AIMaxTokens: 512}
	s := New(db, c)
	t.Cleanup(s.Shutdown)
	f := &fixture{s: s, mid: uuid.NewString(), nid: uuid.NewString(), path: path}
	for i, p := range []*auth.Identity{&f.host, &f.guest, &f.out} {
		p.User = auth.User{ID: uuid.NewString(), Username: fmt.Sprintf("user%d", i), DisplayName: fmt.Sprintf("参会者%d", i)}
		must(t, db, "INSERT INTO users VALUES (?,?,?,?,?,1)", p.User.ID, p.User.Username, p.User.Username, p.User.DisplayName, "PRIVATE_PASSWORD_HASH")
	}
	must(t, db, "INSERT INTO meetings(id,meeting_no,title,host_user_id,livekit_room_name,status,started_at,ended_at,client_request_id) VALUES (?,'123456789','真实会议',?,?,'ended',1,2,'first')", f.mid, f.host.User.ID, f.mid)
	must(t, db, "INSERT INTO meeting_members VALUES (?,?,?,?,1,2)", uuid.NewString(), f.mid, f.guest.User.ID, f.guest.User.DisplayName)
	must(t, db, "INSERT INTO chat_messages(meeting_id,sender_user_id,sender_name_snapshot,client_message_id,content,created_at) VALUES (?,?,'甲','one','确定采用方案A。甲在2026-12-01完成初稿。',1)", f.mid, f.host.User.ID)
	must(t, db, "INSERT INTO meeting_notes VALUES (?,?,?,'已保存的真实笔记',1,2)", f.nid, f.mid, f.host.User.ID)
	return f
}
func must(t *testing.T, db *sql.DB, q string, a ...any) {
	t.Helper()
	if _, e := db.Exec(q, a...); e != nil {
		t.Fatal(e)
	}
}

const valid = `{"summary":"方案A已确定","topics":["方案"],"decisions":[{"text":"采用方案A","sourceIds":["message:1"]}],"actionItems":[{"task":"完成初稿","owner":"甲","dueDate":"2026-12-01","sourceIds":["message:1"]}],"openQuestions":[],"limitations":["不含语音转写"]}`

func respond(w http.ResponseWriter, content string, finish string) {
	json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": finish, "message": map[string]string{"content": content}}}})
}
func (f *fixture) gen(t *testing.T, in GenerateInput) Job {
	t.Helper()
	j, e := f.s.Generate(context.Background(), f.host, f.mid, in, "safe-request-id")
	if e != nil {
		t.Fatal(e)
	}
	return j
}
func input() GenerateInput { return GenerateInput{RequestID: uuid.NewString(), Consent: true} }
func wait(t *testing.T, f *fixture, id string) Job {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		j, e := scanJob(f.s.DB.QueryRow("SELECT "+jobColumns+" FROM summary_jobs WHERE id=?", id))
		if e != nil {
			t.Fatal(e)
		}
		if j.Status != "running" {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("task did not terminate")
	return Job{}
}
func errCode(t *testing.T, e error, code string) {
	t.Helper()
	var a *apperr.Error
	if !errors.As(e, &a) || a.Code != code {
		t.Fatalf("want %s got %v", code, e)
	}
}
func TestRealHTTPAdapterFrozenSourcesIdempotencyAndExport(t *testing.T) {
	var calls atomic.Int32
	gate := make(chan struct{})
	var sent string
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-private-key" {
			t.Error("bad upstream authorization/path")
		}
		b, _ := io.ReadAll(r.Body)
		sent = string(b)
		<-gate
		respond(w, valid, "stop")
	})
	// A different meeting's data and password/token-like metadata must never leave.
	other := uuid.NewString()
	must(t, f.s.DB, "INSERT INTO meetings(id,meeting_no,title,host_user_id,livekit_room_name,status,started_at,client_request_id) VALUES (?,'987654321','别的会议',?,?,'active',1,'other')", other, f.host.User.ID, other)
	must(t, f.s.DB, "INSERT INTO chat_messages(meeting_id,sender_user_id,sender_name_snapshot,client_message_id,content,created_at) VALUES (?,?,'别场','other','OTHER_MEETING_PRIVATE',1)", other, f.host.User.ID)
	in := input()
	j := f.gen(t, in)
	alias := input()
	aliasJob := f.gen(t, alias)
	if aliasJob.ID != j.ID {
		t.Fatal("running alias not reused")
	}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := f.s.Generate(context.Background(), f.host, f.mid, input(), "concurrent")
			if e != nil {
				t.Error(e)
			}
			ids <- v.ID
		}()
	}
	wg.Wait()
	close(ids)
	for id := range ids {
		if id != j.ID {
			t.Fatal("duplicate task", id)
		}
	}
	must(t, f.s.DB, "UPDATE meeting_notes SET content='生成期间更新',version=2 WHERE id=?", f.nid)
	close(gate)
	saved := wait(t, f, j.ID)
	if saved.Status != "succeeded" || calls.Load() != 1 || saved.Result == nil {
		t.Fatal(saved, calls.Load())
	}
	if !strings.Contains(sent, "已保存的真实笔记") || strings.Contains(sent, "生成期间更新") || strings.Contains(sent, "OTHER_MEETING_PRIVATE") || strings.Contains(sent, "PRIVATE_PASSWORD_HASH") || strings.Contains(sent, "test-private-key") {
		t.Fatal("source privacy/snapshot violation")
	}
	aliasAgain := f.gen(t, alias)
	if aliasAgain.ID != j.ID || calls.Load() != 1 {
		t.Fatal("lost deduplicated response created another task")
	}
	same := f.gen(t, in)
	if same.ID != j.ID {
		t.Fatal("lost response duplicated request")
	}
	view, e := f.s.View(context.Background(), f.guest, f.mid)
	if e != nil || view.SourceHash == saved.SourceHash || view.CanGenerate {
		t.Fatal("source update or permissions lost", e)
	}
	md, e := f.s.Export(context.Background(), f.guest, f.mid, j.ID)
	if e != nil || !strings.Contains(md, "已保存的真实笔记") || strings.Contains(md, "生成期间更新") || !strings.Contains(md, "不含语音转写") {
		t.Fatal("bad frozen export", e)
	}
	next := wait(t, f, f.gen(t, input()).ID)
	if next.ID == j.ID || calls.Load() != 2 {
		t.Fatal("new source not generated")
	}
	reused := f.gen(t, input())
	if reused.ID != next.ID || calls.Load() != 2 {
		t.Fatal("same hash not reused")
	}
	// A deliberate regeneration requires a new operation; same operation remains idempotent.
	force := input()
	force.Regenerate = true
	forced := wait(t, f, f.gen(t, force).ID)
	if forced.ID == next.ID || calls.Load() != 3 {
		t.Fatal("explicit regeneration missing")
	}
	_, e = f.s.Generate(context.Background(), f.host, f.mid, GenerateInput{RequestID: uuid.NewString(), Consent: true, Regenerate: true}, "limit")
	errCode(t, e, "AI_RATE_LIMITED")
	for _, who := range []auth.Identity{f.out} {
		_, e = f.s.View(context.Background(), who, f.mid)
		errCode(t, e, "FORBIDDEN")
		_, e = f.s.Export(context.Background(), who, f.mid, j.ID)
		errCode(t, e, "FORBIDDEN")
	}
	_, e = f.s.Generate(context.Background(), f.guest, f.mid, input(), "guest")
	errCode(t, e, "HOST_REQUIRED")
	_, e = f.s.Export(context.Background(), f.host, other, j.ID)
	errCode(t, e, "SUMMARY_NOT_FOUND")
}
func TestSummaryGuardsNoConfigurationEmptyOversizeAndEnded(t *testing.T) {
	var calls atomic.Int32
	f := setup(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); respond(w, valid, "stop") })
	f.s.Config.AIEnabled = false
	_, e := f.s.Generate(context.Background(), f.host, f.mid, input(), "missing")
	errCode(t, e, "AI_NOT_CONFIGURED")
	md, e := f.s.Export(context.Background(), f.guest, f.mid, "")
	if e != nil || !strings.Contains(md, "会议记录") || strings.Contains(md, "# AI会议纪要") {
		t.Fatal(md, e)
	}
	f.s.Config.AIEnabled = true
	must(t, f.s.DB, "UPDATE meetings SET status='active' WHERE id=?", f.mid)
	_, e = f.s.Generate(context.Background(), f.host, f.mid, input(), "active")
	errCode(t, e, "MEETING_NOT_ENDED")
	must(t, f.s.DB, "UPDATE meetings SET status='ended' WHERE id=?", f.mid)
	noConsent := input()
	noConsent.Consent = false
	_, e = f.s.Generate(context.Background(), f.host, f.mid, noConsent, "")
	errCode(t, e, "VALIDATION_ERROR")
	f.s.Config.AIMaxSourceChars = 2
	_, e = f.s.Generate(context.Background(), f.host, f.mid, input(), "chars")
	errCode(t, e, "SOURCE_TOO_LARGE")
	f.s.Config.AIMaxSourceChars = 20000
	f.s.Config.AIMaxSourceBytes = 10
	_, e = f.s.Generate(context.Background(), f.host, f.mid, input(), "bytes")
	errCode(t, e, "SOURCE_TOO_LARGE")
	f.s.Config.AIMaxSourceBytes = 80000
	must(t, f.s.DB, "DELETE FROM chat_messages WHERE meeting_id=?", f.mid)
	must(t, f.s.DB, "UPDATE meeting_notes SET content='  ' WHERE id=?", f.nid)
	_, e = f.s.Generate(context.Background(), f.host, f.mid, input(), "empty")
	errCode(t, e, "SOURCE_EMPTY")
	if calls.Load() != 0 {
		t.Fatal("guard called upstream")
	}
}
func TestDeepSeekFailuresAreSafeAndManuallyRetryable(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		status                int
		content, finish, code string
	}{
		{"auth", 401, "private-secret", "", "AI_AUTH"}, {"permission", 403, "", "", "AI_AUTH"}, {"quota", 402, "", "", "AI_QUOTA"}, {"rate", 429, "", "", "AI_UPSTREAM_RATE_LIMITED"}, {"service", 503, "", "", "AI_UNAVAILABLE"}, {"model", 404, "", "", "AI_REQUEST_REJECTED"},
		{"empty", 200, "", "stop", "AI_EMPTY_RESPONSE"}, {"truncated", 200, valid, "length", "AI_TRUNCATED"}, {"json", 200, "private-secret", "stop", "AI_INVALID_RESULT"}, {"unknown source", 200, strings.ReplaceAll(valid, "message:1", "message:999"), "stop", "AI_INVALID_RESULT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var n atomic.Int32
			f := setup(t, func(w http.ResponseWriter, r *http.Request) {
				if n.Add(1) == 1 {
					w.WriteHeader(tc.status)
					if tc.status == 200 {
						respond(w, tc.content, tc.finish)
					} else {
						io.WriteString(w, tc.content)
					}
					return
				}
				respond(w, valid, "stop")
			})
			first := wait(t, f, f.gen(t, input()).ID)
			if first.Status != "failed" || first.ErrorCode != tc.code || first.Result != nil || strings.Contains(first.ErrorMessage, "private-secret") {
				t.Fatal(first)
			}
			second := wait(t, f, f.gen(t, input()).ID)
			if second.Status != "succeeded" || n.Load() != 2 {
				t.Fatal(second)
			}
		})
	}
}
func TestTimeoutRestartAndResultPersistence(t *testing.T) {
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(1500 * time.Millisecond):
			respond(w, valid, "stop")
		}
	})
	j := wait(t, f, f.gen(t, input()).ID)
	if j.ErrorCode != "AI_TIMEOUT" || j.Status != "failed" {
		t.Fatal(j)
	}
	must(t, f.s.DB, "UPDATE summary_jobs SET status='running',result_json='' WHERE id=?", j.ID)
	f.s.Shutdown()
	f.s.DB.Close()
	db, e := store.Open(f.path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	next := New(db, f.s.Config)
	defer next.Shutdown()
	if e = next.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	v, e := next.View(context.Background(), f.host, f.mid)
	if e != nil || v.Jobs[0].ErrorCode != "SERVER_RESTART" {
		t.Fatal(v, e)
	}
}
func TestStructuredOutputRejectsFabricatedOwnerDateAndUnsafeShapes(t *testing.T) {
	snap := Snapshot{Entries: []Entry{{ID: "message:1", Content: "甲在2026-12-01完成初稿。"}}}
	for _, raw := range []string{strings.Replace(valid, `"owner":"甲"`, `"owner":"不存在的人"`, 1), strings.Replace(valid, "2026-12-01", "2027-12-01", 1), strings.Replace(valid, `"owner":"甲",`, "", 1), strings.Replace(valid, `"topics":["方案"]`, `"topics":null`, 1), strings.Replace(valid, "不含语音转写", "完整录音纪要", 1), valid + " {}"} {
		if _, e := validateResult(raw, snap); e == nil {
			t.Fatal("accepted invalid output", raw)
		}
	}
	if _, e := validateResult(valid, snap); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(plain("<script>[x](javascript:bad)</script>"), "<script>") {
		t.Fatal("unsafe Markdown")
	}
}
func TestShutdownCancelsWorkAndAllowsRestartRetry(t *testing.T) {
	entered := make(chan struct{})
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	j := f.gen(t, input())
	<-entered
	f.s.Shutdown()
	done := wait(t, f, j.ID)
	if done.Status != "failed" || done.ErrorCode != "SERVER_RESTART" {
		t.Fatal(done)
	}
}
func TestSavedResultSurvivesReopenAndExpiredTaskCanBeRetried(t *testing.T) {
	f := setup(t, func(w http.ResponseWriter, r *http.Request) { respond(w, valid, "stop") })
	j := wait(t, f, f.gen(t, input()).ID)
	f.s.Shutdown()
	f.s.DB.Close()
	db, e := store.Open(f.path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	next := New(db, f.s.Config)
	defer next.Shutdown()
	if e = next.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	v, e := next.View(context.Background(), f.guest, f.mid)
	if e != nil || v.Jobs[0].Result == nil || v.Jobs[0].ID != j.ID {
		t.Fatal(v, e)
	}
	must(t, db, "UPDATE summary_jobs SET status='running',created_at=? WHERE id=?", time.Now().Add(-time.Hour).UnixMilli(), j.ID)
	v, e = next.View(context.Background(), f.host, f.mid)
	if e != nil || v.Jobs[0].ErrorCode != "AI_RESULT_UNCONFIRMED" {
		t.Fatal(v, e)
	}
	// Existing note API remains the source of truth.
	_, e = (&meeting.Service{DB: db}).SaveNote(context.Background(), f.host, f.mid, meeting.NoteInput{Content: "新版真实笔记", Version: 1})
	if e != nil {
		t.Fatal(e)
	}
}

func TestGlobalWorkersBoundedAndRequestCancellationDoesNotLoseTask(t *testing.T) {
	gate := make(chan struct{})
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		<-gate
		respond(w, `{"summary":"真实测试记录","topics":[],"decisions":[],"actionItems":[],"openQuestions":[],"limitations":["不含语音转写"]}`, "stop")
	})
	f.s.Config.AITimeout = 3
	first := f.gen(t, input())
	var others []string
	for i := 0; i < 2; i++ {
		id := uuid.NewString()
		others = append(others, id)
		must(t, f.s.DB, "INSERT INTO meetings(id,meeting_no,title,host_user_id,livekit_room_name,status,started_at,ended_at,client_request_id) VALUES (?,?,'并行测试',?,?,'ended',1,2,?)", id, fmt.Sprintf("12345678%d", i), f.host.User.ID, id, id)
		must(t, f.s.DB, "INSERT INTO meeting_notes VALUES (?,?,?,'真实测试记录',1,2)", uuid.NewString(), id, f.host.User.ID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	second, e := f.s.Generate(ctx, f.host, others[0], input(), "second")
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	_, e = f.s.Generate(context.Background(), f.host, others[1], input(), "third")
	errCode(t, e, "AI_BUSY")
	close(gate)
	if wait(t, f, first.ID).Status != "succeeded" || wait(t, f, second.ID).Status != "succeeded" {
		t.Fatal("request cancellation lost durable task")
	}
}
func TestRedirectNeverForwardsPrivateKey(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	j := wait(t, f, f.gen(t, input()).ID)
	if j.Status != "failed" || forwarded.Load() != 0 {
		t.Fatal("credential redirect not blocked", j)
	}
}
func TestDatabaseWriteFailureNeverReportsSuccess(t *testing.T) {
	gate := make(chan struct{})
	f := setup(t, func(w http.ResponseWriter, r *http.Request) { <-gate; respond(w, valid, "stop") })
	j := f.gen(t, input())
	must(t, f.s.DB, "CREATE TRIGGER fail_summary_commit BEFORE UPDATE OF status ON summary_jobs BEGIN SELECT RAISE(ABORT,'private database error'); END")
	close(gate)
	f.s.Shutdown()
	var status string
	f.s.DB.QueryRow("SELECT status FROM summary_jobs WHERE id=?", j.ID).Scan(&status)
	if status != "running" {
		t.Fatal("fake success on DB failure", status)
	}
	must(t, f.s.DB, "DROP TRIGGER fail_summary_commit")
	next := New(f.s.DB, f.s.Config)
	defer next.Shutdown()
	if e := next.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	v, e := next.View(context.Background(), f.host, f.mid)
	if e != nil || v.Jobs[0].Status != "failed" || v.Jobs[0].Result != nil {
		t.Fatal(v, e)
	}
}
