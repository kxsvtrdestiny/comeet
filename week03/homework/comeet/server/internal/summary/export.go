package summary

import (
	"comeet/server/internal/apperr"
	"comeet/server/internal/auth"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Source/model text is always plain text in the UI. Export escapes Markdown/HTML
// metacharacters and never embeds raw HTML, scripts, links or image directives.
func plain(v string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "#", "\\#", "!", "\\!", "|", "\\|", "\r", "")
	return strings.ReplaceAll(r.Replace(v), "\n", "\n\n")
}
func (s *Service) Export(ctx context.Context, who auth.Identity, mid, jobID string) (string, error) {
	m, e := s.authorize(ctx, who, mid)
	if e != nil {
		return "", e
	}
	var snap Snapshot
	var j Job
	if jobID != "" {
		var raw string
		if e = s.DB.QueryRowContext(ctx, "SELECT source_snapshot FROM summary_jobs WHERE id=? AND meeting_id=? AND status='succeeded'", jobID, mid).Scan(&raw); e != nil {
			return "", apperr.New(404, "SUMMARY_NOT_FOUND", "未找到本场已成功的纪要")
		}
		if e = json.Unmarshal([]byte(raw), &snap); e != nil {
			return "", e
		}
		j, e = scanJob(s.DB.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM summary_jobs WHERE id=? AND meeting_id=?", jobID, mid))
		if e != nil {
			return "", e
		}
	} else {
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			return "", e
		}
		defer tx.Rollback()
		snap, _, _, e = s.snapshotBudget(ctx, tx, mid, 200000, 1000000)
		if e != nil {
			return "", e
		}
		if e = tx.Commit(); e != nil {
			return "", e
		}
	}
	var b strings.Builder
	kind := "会议记录"
	if j.Result != nil {
		kind = "AI会议纪要"
	}
	fmt.Fprintf(&b, "# %s：%s\n\n%s\n\n会议号：%s\n\n开始时间：%s\n\n来源范围：本场全部聊天和已保存手动笔记；不含语音转写。\n\n", kind, plain(m.Title), Disclaimer, snap.MeetingNo, time.UnixMilli(snap.StartedAt).UTC().Format(time.RFC3339))
	if j.Result != nil {
		fmt.Fprintf(&b, "模型：%s\n\n生成时间：%s\n\n来源SHA-256：%s\n\n以下纪要对应生成时冻结版本，之后修改的笔记不自动合并。\n\n## 概述\n\n%s\n\n", plain(j.Model), time.UnixMilli(*j.FinishedAt).UTC().Format(time.RFC3339), j.SourceHash, plain(j.Result.Summary))
		fmt.Fprintln(&b, "## 议题")
		for _, v := range j.Result.Topics {
			fmt.Fprintf(&b, "\n- %s\n", plain(v))
		}
		fmt.Fprintln(&b, "\n## 决策")
		for _, v := range j.Result.Decisions {
			fmt.Fprintf(&b, "\n- %s（来源：%s）\n", plain(v.Text), plain(strings.Join(v.SourceIDs, ", ")))
		}
		fmt.Fprintln(&b, "\n## 待办")
		for _, v := range j.Result.ActionItems {
			owner, date := "未明确", "未明确"
			if v.Owner != nil {
				owner = *v.Owner
			}
			if v.DueDate != nil {
				date = *v.DueDate
			}
			fmt.Fprintf(&b, "\n- %s；负责人：%s；截止：%s（来源：%s）\n", plain(v.Task), plain(owner), plain(date), plain(strings.Join(v.SourceIDs, ", ")))
		}
		fmt.Fprintln(&b, "\n## 待确认")
		for _, v := range j.Result.OpenQuestions {
			fmt.Fprintf(&b, "\n- %s\n", plain(v))
		}
		fmt.Fprintln(&b, "\n## 局限")
		for _, v := range j.Result.Limitations {
			fmt.Fprintf(&b, "\n- %s\n", plain(v))
		}
	}
	fmt.Fprintln(&b, "\n## 来源记录")
	for _, x := range snap.Entries {
		fmt.Fprintf(&b, "\n### %s · %s", plain(x.ID), plain(x.Author))
		if x.Version > 0 {
			fmt.Fprintf(&b, " · 版本%d", x.Version)
		}
		fmt.Fprintf(&b, "\n\n%s\n", plain(x.Content))
	}
	return b.String(), nil
}
