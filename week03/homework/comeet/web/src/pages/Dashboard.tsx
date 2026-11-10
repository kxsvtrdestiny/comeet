import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { Link, useNavigate, useLocation } from "react-router-dom";
import { Plus, LogIn } from "lucide-react";
import { Empty } from "../components/Common";
import { post, request } from "../services/api";
import {
  staticPreview,
  formatMeetingNo,
  type Meeting,
  type MeetingList,
  type Preparation,
} from "../services/types";
export default function Dashboard() {
  return staticPreview ? <StaticDashboard /> : <LiveDashboard />;
}
function StaticDashboard() {
  return (
    <>
      <h1>工作台</h1>
      <section className="card">
        <p>
          静态测试站未连接业务服务。注册、登录和会议操作请在 Windows
          本机运行前后端后验收。
        </p>
        <a
          className="text-button"
          href={`${import.meta.env.BASE_URL}comeet-source.zip`}
          download
        >
          下载第三阶段源码 ZIP
        </a>
        <Link className="text-button" to="/meetings/preview/room">
          会议室界面预览
        </Link>
      </section>
    </>
  );
}
function LiveDashboard() {
  const nav = useNavigate();
  const location = useLocation();
  const [title, setTitle] = useState(""),
    [number, setNumber] = useState(""),
    [creating, setCreating] = useState(false),
    [resolving, setResolving] = useState(false),
    [createError, setCreateError] = useState(""),
    [resolveError, setResolveError] = useState("");
  const createRequest = useRef<{ title: string; id: string } | undefined>(
    undefined,
  );
  const [items, setItems] = useState<Meeting[]>([]),
    [next, setNext] = useState(""),
    [more, setMore] = useState(false),
    [loading, setLoading] = useState(true),
    [error, setError] = useState(""),
    [status, setStatus] = useState(""),
    [search, setSearch] = useState(""),
    [keyword, setKeyword] = useState("");
  const controller = useRef<AbortController | null>(null);
  const load = useCallback(
    async (cursor = "", append = false) => {
      controller.current?.abort();
      const c = new AbortController();
      controller.current = c;
      setLoading(true);
      setError("");
      try {
        const q = new URLSearchParams({ limit: "20", status, keyword, cursor });
        const result = await request<MeetingList>("/meetings?" + q, {
          signal: c.signal,
        });
        if (!c.signal.aborted) {
          setItems((old) =>
            append ? [...old, ...result.items] : result.items,
          );
          setNext(result.nextCursor);
          setMore(result.hasMore);
        }
      } catch (e) {
        if (!c.signal.aborted) setError((e as Error).message);
      } finally {
        if (!c.signal.aborted) setLoading(false);
      }
    },
    [status, keyword],
  );
  useEffect(() => {
    void load();
    return () => controller.current?.abort();
  }, [load]);
  async function create(e: FormEvent) {
    e.preventDefault();
    if (creating) return;
    setCreating(true);
    setCreateError("");
    if (!createRequest.current || createRequest.current.title !== title)
      createRequest.current = { title, id: crypto.randomUUID() };
    try {
      const m = await post<Meeting>("/meetings", {
        title,
        clientRequestId: createRequest.current.id,
      });
      createRequest.current = undefined;
      nav(`/meetings/${m.id}/prepare`);
    } catch (e) {
      setCreateError((e as Error).message);
    } finally {
      setCreating(false);
    }
  }
  async function resolve(e: FormEvent) {
    e.preventDefault();
    setResolving(true);
    setResolveError("");
    try {
      const m = await post<Preparation>("/meetings/resolve", {
        meetingNo: number,
      });
      nav(`/meetings/${m.id}/prepare`);
    } catch (e) {
      setResolveError((e as Error).message);
    } finally {
      setResolving(false);
    }
  }
  return (
    <>
      <h1>工作台</h1>
      {location.state?.leaveNotice && (
        <p role="status">{location.state.leaveNotice}</p>
      )}
      <div className="actions">
        <form className="card action-card" onSubmit={create}>
          <div className="action-icon">
            <Plus />
          </div>
          <h2>发起即时会议</h2>
          <label>
            会议标题（可选）
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              maxLength={80}
              placeholder="未命名会议"
            />
          </label>
          {createError && (
            <p role="alert" className="error">
              {createError}
            </p>
          )}
          <button className="button" disabled={creating}>
            {creating ? "创建中…" : "创建会议"}
          </button>
        </form>
        <form className="card action-card" onSubmit={resolve}>
          <div className="action-icon secondary">
            <LogIn />
          </div>
          <h2>通过会议号加入</h2>
          <label>
            会议号
            <input
              value={number}
              onChange={(e) => setNumber(e.target.value)}
              required
              placeholder="123-456-789"
              maxLength={32}
              inputMode="numeric"
            />
          </label>
          {resolveError && (
            <p role="alert" className="error">
              {resolveError}
            </p>
          )}
          <button className="button" disabled={resolving}>
            {resolving ? "查询中…" : "查询会议"}
          </button>
        </form>
      </div>
      <section className="card">
        <div className="section-title">
          <h2>我的会议</h2>
          <button
            className="text-button"
            disabled={loading}
            onClick={() => load()}
          >
            刷新列表
          </button>
        </div>
        <div className="list-filters">
          <label>
            会议状态
            <select value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="">全部</option>
              <option value="active">进行中</option>
              <option value="ended">已结束</option>
            </select>
          </label>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              setKeyword(search);
            }}
          >
            <label>
              搜索会议
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                maxLength={80}
                placeholder="标题或会议号"
              />
            </label>
            <button className="button">搜索</button>
          </form>
        </div>
        {loading && <p role="status">正在加载会议…</p>}
        {error && (
          <div role="alert">
            <p className="error">{error}</p>
            <button className="text-button" onClick={() => load()}>
              重试
            </button>
          </div>
        )}
        {!loading && !error && items.length === 0 && (
          <Empty title="暂无会议">
            没有符合条件的会议，可创建会议或通过会议号查询。
          </Empty>
        )}
        <div className="meeting-list">
          {items.map((m) => (
            <article className="meeting-row" key={m.id}>
              <div>
                <h3>{m.title}</h3>
                <p>
                  {formatMeetingNo(m.meetingNo)} · 主持人：{m.hostName}
                </p>
                <p className="muted">
                  {new Date(m.startedAt).toLocaleString("zh-CN")}
                </p>
              </div>
              <div className="meeting-row-actions">
                <span className="tag">
                  {m.status === "active"
                    ? "进行中"
                    : m.status === "ending"
                      ? "结束中"
                      : "已结束"}
                </span>
                <Link to={`/meetings/${m.id}`}>查看详情</Link>
                {m.status === "active" && (
                  <Link to={`/meetings/${m.id}/prepare`}>入会准备</Link>
                )}
              </div>
            </article>
          ))}
        </div>
        {more && (
          <button
            className="text-button"
            disabled={loading}
            onClick={() => load(next, true)}
          >
            加载更多
          </button>
        )}
      </section>
    </>
  );
}
