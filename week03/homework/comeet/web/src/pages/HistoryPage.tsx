import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { request } from "../services/api";
import { useRemote } from "../services/useRemote";
import {
  staticPreview,
  formatMeetingNo,
  type Meeting,
  type MeetingList,
} from "../services/types";
import { ManualNotes } from "../components/ManualNotes";
import type { Message } from "../services/room";
export default function HistoryPage({ detail = false }: { detail?: boolean }) {
  return staticPreview ? (
    <>
      <h1>{detail ? "会议记录详情" : "历史会议"}</h1>
      <p>静态预览不查询私人会议记录。</p>
    </>
  ) : detail ? (
    <HistoryDetail />
  ) : (
    <HistoryList />
  );
}
function duration(ms: number) {
  const s = Math.floor(ms / 1000);
  return `${Math.floor(s / 3600)}小时${Math.floor(s / 60) % 60}分${s % 60}秒`;
}
function HistoryList() {
  const [params, setParams] = useSearchParams();
  const status = params.get("status") ?? "ended",
    keyword = params.get("keyword") || "";
  const [search, setSearch] = useState(keyword),
    [items, setItems] = useState<Meeting[]>([]),
    [cursor, setCursor] = useState(""),
    [more, setMore] = useState(false),
    [loading, setLoading] = useState(false),
    [error, setError] = useState("");
  const active = useRef<AbortController | null>(null);
  const generation = useRef(0);
  async function load(after = "") {
    active.current?.abort();
    const control = new AbortController();
    active.current = control;
    const gen = ++generation.current;
    setLoading(true);
    setError("");
    try {
      const data = await request<MeetingList>(
        `/meetings?${new URLSearchParams({ status, keyword, cursor: after, limit: "20" })}`,
        { signal: control.signal },
      );
      if (gen !== generation.current || control.signal.aborted) return;
      setItems((p) =>
        after
          ? [
              ...p,
              ...data.items.filter((m) => !p.some((old) => old.id === m.id)),
            ]
          : data.items,
      );
      setCursor(data.nextCursor);
      setMore(data.hasMore);
    } catch (e) {
      if (!control.signal.aborted) setError((e as Error).message);
    } finally {
      if (!control.signal.aborted) setLoading(false);
    }
  }
  useEffect(() => {
    setItems([]);
    setCursor("");
    setMore(false);
    setSearch(keyword);
    void load();
    return () => active.current?.abort();
  }, [status, keyword]);
  function filter(e: FormEvent) {
    e.preventDefault();
    setParams({ status, keyword: search.trim() });
  }
  return (
    <>
      <h1>历史会议</h1>
      <section className="card">
        <div className="history-filters">
          <label>
            记录状态
            <select
              aria-label="历史会议状态"
              value={status}
              onChange={(e) => setParams({ status: e.target.value, keyword })}
            >
              <option value="ended">已结束</option>
              <option value="ending">结束中</option>
              <option value="active">进行中</option>
              <option value="">全部</option>
            </select>
          </label>
          <form onSubmit={filter}>
            <label>
              搜索历史会议
              <input
                value={search}
                maxLength={80}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="标题或会议号"
              />
            </label>
            <button className="button">搜索历史</button>
          </form>
        </div>
        {loading && <p role="status">正在加载历史会议…</p>}
        {error && (
          <div role="alert">
            <p className="error">{error}</p>
            <button onClick={() => void load(items.length ? cursor : "")}>
              重试读取历史
            </button>
          </div>
        )}
        {!loading && !error && !items.length && <p>暂无符合条件的会议记录</p>}
        <ul className="history-list">
          {items.map((m) => (
            <li key={m.id}>
              <h2>
                <Link to={`/history/${m.id}`}>{m.title}</Link>
              </h2>
              <p>
                会议号：{formatMeetingNo(m.meetingNo)} · 主持人：{m.hostName}
              </p>
              <p>
                {new Date(m.startedAt).toLocaleString("zh-CN")} ·{" "}
                {m.status === "ended"
                  ? "已结束"
                  : m.status === "ending"
                    ? "结束中"
                    : "进行中"}
              </p>
            </li>
          ))}
        </ul>
        {more && (
          <button disabled={loading} onClick={() => void load(cursor)}>
            加载更多会议
          </button>
        )}
      </section>
    </>
  );
}
interface Attendance {
  sessionId: string;
  joinedAt: number;
  leftAt: number | null;
  durationMillis: number;
}
interface RecordData {
  meeting: Meeting;
  durationMillis: number;
  attendees: {
    userId: string;
    displayName: string;
    isHost: boolean;
    durationMillis: number;
    intervals: Attendance[];
  }[];
}
function HistoryDetail() {
  const { id = "" } = useParams();
  const { data, loading, error, retry } = useRemote<RecordData>(
    `/meetings/${encodeURIComponent(id)}/record`,
  );
  return (
    <>
      <Link to="/history">返回历史会议</Link>
      <h1>会议记录详情</h1>
      {loading ? (
        <p role="status">正在读取真实参会记录…</p>
      ) : error ? (
        <div role="alert">
          <p className="error">{error}</p>
          <button onClick={retry}>重试读取记录</button>
        </div>
      ) : (
        data && (
          <>
            <section className="card">
              <h2>{data.meeting.title}</h2>
              <p>
                会议号：{formatMeetingNo(data.meeting.meetingNo)} · 主持人：
                {data.meeting.hostName}
              </p>
              <p>
                会议时长：{duration(data.durationMillis)}
                {data.meeting.status !== "ended" ? "（截至本次读取）" : ""}
              </p>
              <p>
                开始：{new Date(data.meeting.startedAt).toLocaleString("zh-CN")}{" "}
                · 结束：
                {data.meeting.endedAt
                  ? new Date(data.meeting.endedAt).toLocaleString("zh-CN")
                  : "尚未结束"}
              </p>
              <Link to={`/meetings/${id}`}>会议详情与操作</Link>
              <h3>实际参会记录</h3>
              {!data.attendees.length && (
                <p>尚无实际参会记录，准备/预留不计入参会。</p>
              )}
              {data.attendees.map((a) => (
                <article className="attendance-row" key={a.userId}>
                  <strong>
                    {a.displayName}
                    {a.isHost ? " · 主持人" : ""}
                  </strong>
                  <p>累计参会：{duration(a.durationMillis)}</p>
                  <details>
                    <summary>查看参会区间（{a.intervals.length}段）</summary>
                    {a.intervals.map((v, i) => (
                      <p key={`${v.sessionId}:${i}`}>
                        {new Date(v.joinedAt).toLocaleString("zh-CN")} —{" "}
                        {v.leftAt
                          ? new Date(v.leftAt).toLocaleString("zh-CN")
                          : "仍在会中"}{" "}
                        · {duration(v.durationMillis)}
                      </p>
                    ))}
                  </details>
                </article>
              ))}
            </section>
            <section className="card">
              <HistoryChat meetingId={id} />
            </section>
            <section className="card">
              <ManualNotes meetingId={id} />
            </section>
          </>
        )
      )}
    </>
  );
}
function HistoryChat({ meetingId }: { meetingId: string }) {
  const [messages, setMessages] = useState<Message[]>([]),
    [before, setBefore] = useState(0),
    [more, setMore] = useState(false),
    [loading, setLoading] = useState(false),
    [error, setError] = useState("");
  const abort = useRef<AbortController | null>(null);
  async function load(cursor = 0) {
    abort.current?.abort();
    const c = new AbortController();
    abort.current = c;
    setLoading(true);
    setError("");
    try {
      const page = await request<{
        items: Message[];
        nextCursor: number;
        hasMore: boolean;
      }>(
        `/meetings/${meetingId}/messages?limit=50${cursor ? `&beforeId=${cursor}` : ""}`,
        { signal: c.signal },
      );
      if (c.signal.aborted) return;
      setMessages((p) =>
        cursor
          ? [
              ...page.items.filter((m) => !p.some((old) => old.id === m.id)),
              ...p,
            ]
          : page.items,
      );
      setBefore(page.nextCursor);
      setMore(page.hasMore);
    } catch (e) {
      if (!c.signal.aborted) setError((e as Error).message);
    } finally {
      if (!c.signal.aborted) setLoading(false);
    }
  }
  useEffect(() => {
    setMessages([]);
    void load();
    return () => abort.current?.abort();
  }, [meetingId]);
  return (
    <>
      <h2>真实聊天记录</h2>
      {error && (
        <p role="alert" className="error">
          {error}
          <button onClick={() => void load(messages.length ? before : 0)}>
            重试聊天记录
          </button>
        </p>
      )}
      {more && (
        <button disabled={loading} onClick={() => void load(before)}>
          加载更早聊天
        </button>
      )}
      {loading && <p role="status">正在加载聊天…</p>}
      {!loading && !messages.length && !error && <p>本场暂无聊天记录</p>}
      <div className="history-chat">
        {messages.map((m) => (
          <article key={m.id}>
            <strong>{m.senderName}</strong>
            <time>{new Date(m.createdAt).toLocaleString("zh-CN")}</time>
            <p>{m.content}</p>
          </article>
        ))}
      </div>
    </>
  );
}
