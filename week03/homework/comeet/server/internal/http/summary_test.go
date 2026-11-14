package httpapi

import (
	"comeet/server/internal/realtime"
	"comeet/server/internal/summary"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSummaryHTTPAuthorizationAsyncSaveAndExport(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": `{"summary":"真实笔记的概述<script>不执行</script>","topics":[],"decisions":[],"actionItems":[],"openQuestions":[],"limitations":["不含语音转写"]}`}}}})
	}))
	defer upstream.Close()
	h := newWSHarness(t)
	host, guest, out := h.user("SummaryHost"), h.user("SummaryGuest"), h.user("SummaryOutsider")
	h.c.AIEnabled = true
	h.c.AIBase = upstream.URL
	h.c.AIKey = "backend-only-private-key"
	h.c.AIModel = "fixture-model"
	h.c.AITimeout = 2
	ai := summary.New(h.db, h.c)
	t.Cleanup(ai.Shutdown)
	h.handler = RouterWithSummary(h.db, h.c, h.hub, ai)
	m := h.create(host.Token, "纪要权限检查")
	r := h.reserve(guest.Token, m.ID)
	h.connect(guest.Token, m.ID, r.SessionID)
	base := "/meetings/" + m.ID + "/summaries"
	for _, method := range []string{"GET", "POST"} {
		status, res := h.call(method, base, "", nil)
		check(t, status, res, 401, "UNAUTHORIZED")
	}
	for _, path := range []string{base, base + "/export"} {
		status, res := h.call("GET", path, out.Token, nil)
		check(t, status, res, 403, "FORBIDDEN")
	}
	in := summary.GenerateInput{RequestID: uuid.NewString(), Consent: true}
	status, res := h.call("POST", base, guest.Token, in)
	check(t, status, res, 403, "HOST_REQUIRED")
	status, res = h.call("POST", base, host.Token, in)
	check(t, status, res, 409, "MEETING_NOT_ENDED")
	status, res = h.call("PUT", "/meetings/"+m.ID+"/notes/me", host.Token, map[string]any{"content": "实际手动笔记", "version": 0})
	check(t, status, res, 200, "OK")
	status, res = h.call("POST", "/meetings/"+m.ID+"/end", host.Token, realtime.ControlInput{RequestID: uuid.NewString()})
	check(t, status, res, 200, "OK")
	status, res = h.call("POST", base, host.Token, map[string]any{"requestId": uuid.NewString(), "consent": true, "baseURL": "https://attacker.invalid"})
	check(t, status, res, 400, "VALIDATION_ERROR")
	status, res = h.call("POST", base, host.Token, in)
	check(t, status, res, 202, "OK")
	job := unpack[summary.Job](t, res)
	deadline := time.Now().Add(3 * time.Second)
	var view summary.View
	for time.Now().Before(deadline) {
		status, res = h.call("GET", base, guest.Token, nil)
		check(t, status, res, 200, "OK")
		view = unpack[summary.View](t, res)
		if len(view.Jobs) > 0 && view.Jobs[0].Status != "running" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(view.Jobs) != 1 || view.Jobs[0].Status != "succeeded" || view.CanGenerate || view.Jobs[0].Result == nil {
		t.Fatal(view)
	}
	status, res = h.call("POST", base, host.Token, in)
	check(t, status, res, 200, "OK")
	if unpack[summary.Job](t, res).ID != job.ID {
		t.Fatal("request not reused")
	}
	status, res = h.call("GET", base+"/export?jobId="+job.ID, guest.Token, nil)
	check(t, status, res, 200, "OK")
	exported := unpack[map[string]string](t, res)
	if !strings.Contains(exported["markdown"], "实际手动笔记") || strings.Contains(exported["markdown"], "<script>") || strings.Contains(string(res.Data), "backend-only-private-key") {
		t.Fatal("export privacy/escaping")
	}
	status, res = h.call("PUT", "/meetings/"+m.ID+"/notes/me", host.Token, map[string]any{"content": "更新后的笔记", "version": 1})
	check(t, status, res, 200, "OK")
	status, res = h.call("GET", base, host.Token, nil)
	check(t, status, res, 200, "OK")
	updated := unpack[summary.View](t, res)
	if updated.SourceHash == updated.Jobs[0].SourceHash {
		t.Fatal("source update not detected")
	}
	other := h.create(host.Token, "其他会议")
	status, res = h.call("GET", "/meetings/"+other.ID+"/summaries/export?jobId="+job.ID, host.Token, nil)
	check(t, status, res, 404, "SUMMARY_NOT_FOUND")
}
