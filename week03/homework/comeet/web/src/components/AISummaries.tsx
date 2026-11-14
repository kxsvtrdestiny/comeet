import { useEffect, useRef, useState } from "react";
import { ApiError, post, request } from "../services/api";
type Result = {
  summary: string;
  topics: string[];
  decisions: { text: string; sourceIds: string[] }[];
  actionItems: {
    task: string;
    owner: string | null;
    dueDate: string | null;
    sourceIds: string[];
  }[];
  openQuestions: string[];
  limitations: string[];
};
type Job = {
  id: string;
  status: "running" | "succeeded" | "failed";
  sourceHash: string;
  model: string;
  result?: Result;
  errorCode?: string;
  errorMessage?: string;
  diagnosticId?: string;
  createdAt: number;
  finishedAt: number | null;
  messageCount: number;
  noteCount: number;
};
type View = {
  configured: boolean;
  canGenerate: boolean;
  reason: string;
  model: string;
  sourceHash: string;
  sourceError?: string;
  maxChars: number;
  maxBytes: number;
  disclaimer: string;
  jobs: Job[];
};
const disclaimer =
  "由 DeepSeek 基于聊天与手动笔记生成，不含语音转写，请核对内容。";
export function AISummaries({
  meetingId,
  revision,
}: {
  meetingId: string;
  revision: number;
}) {
  const [data, setData] = useState<View | null>(null),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(false),
    [busy, setBusy] = useState(false),
    [selected, setSelected] = useState("");
  const active = useRef<AbortController | null>(null),
    mounted = useRef(true),
    busyRef = useRef(false),
    pendingRequest = useRef<string | null>(null);
  const path = `/meetings/${encodeURIComponent(meetingId)}/summaries`;
  async function load() {
    active.current?.abort();
    const c = new AbortController();
    active.current = c;
    setLoading(true);
    try {
      const v = await request<View>(path, { signal: c.signal });
      if (!c.signal.aborted) {
        setData(v);
        setError("");
      }
    } catch (e) {
      if (!c.signal.aborted) setError((e as Error).message);
    } finally {
      if (!c.signal.aborted) setLoading(false);
    }
  }
  useEffect(() => {
    mounted.current = true;
    setData(null);
    setSelected("");
    pendingRequest.current = null;
    void load();
    const focus = () => void load();
    window.addEventListener("focus", focus);
    return () => {
      mounted.current = false;
      active.current?.abort();
      window.removeEventListener("focus", focus);
    };
  }, [meetingId]);
  useEffect(() => {
    if (revision) void load();
  }, [revision]);
  const running = data?.jobs.some((j) => j.status === "running");
  useEffect(() => {
    if (!running) return;
    const timer = setTimeout(() => void load(), 2000);
    return () => clearTimeout(timer);
  }, [data, error, running]);
  const job = data?.jobs.find((j) => j.id === selected) || data?.jobs[0];
  const changed =
    !!job && (job.sourceHash !== data?.sourceHash || !!data?.sourceError);
  async function generate() {
    if (busyRef.current || !data?.canGenerate || running) return;
    if (
      !window.confirm(
        "将本场全部聊天和已保存手动笔记发送至 DeepSeek 生成纪要，不含语音转写。生成或重试可能产生费用，是否继续？",
      )
    )
      return;
    busyRef.current = true;
    setBusy(true);
    setError("");
    const id = pendingRequest.current || crypto.randomUUID();
    pendingRequest.current = id;
    try {
      const result = await post<Job>(path, {
        requestId: id,
        consent: true,
        regenerate: data.jobs.some((j) => j.status === "succeeded"),
      });
      pendingRequest.current = null;
      if (mounted.current) {
        setSelected(result.id);
        await load();
      }
    } catch (e) {
      if (e instanceof ApiError && e.status > 0 && e.status < 500)
        pendingRequest.current = null;
      if (mounted.current) {
        setError(
          (e as Error).message +
            "；若结果未确认，重试会先查询原操作。诊断编号：" +
            (e instanceof ApiError ? e.requestId || "无" : "无"),
        );
      }
    } finally {
      busyRef.current = false;
      if (mounted.current) setBusy(false);
    }
  }
  async function download(jobId = "") {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setError("");
    try {
      const v = await request<{ filename: string; markdown: string }>(
        path + "/export" + (jobId ? `?jobId=${encodeURIComponent(jobId)}` : ""),
      );
      if (!mounted.current) return;
      const url = URL.createObjectURL(
        new Blob([v.markdown], { type: "text/markdown;charset=utf-8" }),
      );
      const a = document.createElement("a");
      a.href = url;
      a.download = v.filename;
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (e) {
      if (mounted.current) setError((e as Error).message);
    } finally {
      busyRef.current = false;
      if (mounted.current) setBusy(false);
    }
  }
  const labels = {
    running: "生成中",
    succeeded: "已成功保存",
    failed: "生成失败",
  };
  return (
    <section className="card ai-summaries" aria-label="DeepSeek会议纪要">
      <h2>DeepSeek 会议纪要</h2>
      <p className="ai-disclaimer">{disclaimer}</p>
      <p>
        仅使用本场聊天及已保存手动笔记；生成前请先保存草稿。AI
        可能遗漏或误解，请逐项核对来源。
      </p>
      {loading && !data && <p role="status">正在读取纪要任务…</p>}
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      <div className="summary-actions">
        <button disabled={loading || busy} onClick={() => void load()}>
          刷新纪要状态
        </button>
        <button disabled={!data || busy} onClick={() => void download()}>
          导出原始会议记录 Markdown
        </button>
        <button
          className="button"
          disabled={!data?.canGenerate || busy || running}
          onClick={() => void generate()}
        >
          {running
            ? "正在生成纪要…"
            : data?.jobs.some((j) => j.status === "succeeded")
              ? "重新生成纪要"
              : data?.jobs[0]?.status === "failed"
                ? "重试生成纪要"
                : "生成 AI 纪要"}
        </button>
      </div>
      {data && (
        <>
          {data.reason && <p role="note">{data.reason}</p>}
          {!data.configured && (
            <p>
              DeepSeek未配置。后端 server/.env：DEEPSEEK_ENABLED=true，填写
              DEEPSEEK_MODEL、DEEPSEEK_API_KEY 后重启。Key
              不放前端、不在聊天中发送。
            </p>
          )}
          <small>
            输入上限：{data.maxChars}字符 / {data.maxBytes}
            字节（保守预算，非精确 token 计数）；不自动截断。模型：
            {data.model || "未设置"}。
          </small>
          {!data.jobs.length && (
            <p>尚无纪要任务；没有模型返回时不会展示示例纪要。</p>
          )}
          {!!data.jobs.length && (
            <label>
              任务记录（最近10次及最近成功）
              <select
                aria-label="纪要任务"
                value={job?.id || ""}
                onChange={(e) => setSelected(e.target.value)}
              >
                {data.jobs.map((j) => (
                  <option key={j.id} value={j.id}>
                    {new Date(j.createdAt).toLocaleString("zh-CN")} ·{" "}
                    {labels[j.status]}
                  </option>
                ))}
              </select>
            </label>
          )}
          {job && (
            <article className="summary-result">
              <p role="status">
                任务状态：{labels[job.status]}
                {job.status === "running" ? "，可离开页面，刷新后继续查看" : ""}
              </p>
              <p>
                来源：{job.messageCount}条聊天、{job.noteCount}份笔记 · 模型：
                {job.model}
              </p>
              {changed && (
                <p className="summary-stale" role="status">
                  记录已更新，可重新生成。当前任务对应旧的冻结来源。
                </p>
              )}
              {job.status === "failed" && (
                <div role="alert">
                  <p className="error">
                    {job.errorMessage}（{job.errorCode}）
                  </p>
                  <p>
                    诊断编号：{job.diagnosticId || job.id}
                    。失败重试可能再次产生费用；不会自动调用。
                  </p>
                </div>
              )}
              {job.result && (
                <>
                  <h3>概述</h3>
                  <p>{job.result.summary}</p>
                  <h3>议题</h3>
                  <ul>
                    {job.result.topics.map((v, i) => (
                      <li key={i}>{v}</li>
                    ))}
                  </ul>
                  <h3>决策</h3>
                  {!job.result.decisions.length && <p>无明确决策</p>}
                  <ul>
                    {job.result.decisions.map((v, i) => (
                      <li key={i}>
                        {v.text}
                        <small>来源：{v.sourceIds.join("、")}</small>
                      </li>
                    ))}
                  </ul>
                  <h3>待办</h3>
                  {!job.result.actionItems.length && <p>无明确待办</p>}
                  <ul>
                    {job.result.actionItems.map((v, i) => (
                      <li key={i}>
                        {v.task}
                        <p>
                          负责人：{v.owner ?? "未明确"} · 截止：
                          {v.dueDate ?? "未明确"}
                        </p>
                        <small>来源：{v.sourceIds.join("、")}</small>
                      </li>
                    ))}
                  </ul>
                  <h3>待确认</h3>
                  <ul>
                    {job.result.openQuestions.map((v, i) => (
                      <li key={i}>{v}</li>
                    ))}
                  </ul>
                  <h3>局限</h3>
                  <ul>
                    {job.result.limitations.map((v, i) => (
                      <li key={i}>{v}</li>
                    ))}
                  </ul>
                  <p>
                    生成时间：
                    {job.finishedAt
                      ? new Date(job.finishedAt).toLocaleString("zh-CN")
                      : "未确认"}
                  </p>
                  <small>来源 SHA-256：{job.sourceHash}</small>
                  <p>
                    <button
                      disabled={busy}
                      onClick={() => void download(job.id)}
                    >
                      导出此纪要 Markdown
                    </button>
                  </p>
                </>
              )}
            </article>
          )}
        </>
      )}
    </section>
  );
}
