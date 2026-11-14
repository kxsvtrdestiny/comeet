import { useEffect, useRef, useState } from "react";
import { ApiError, request } from "../services/api";
import { useAuth } from "../stores/auth";
export interface Note {
  id: string;
  authorId: string;
  authorName: string;
  content: string;
  version: number;
  updatedAt: number;
}
export function ManualNotes({
  meetingId,
  compact = false,
}: {
  meetingId: string;
  compact?: boolean;
}) {
  const user = useAuth((s) => s.session?.user.id);
  const [notes, setNotes] = useState<Note[]>([]),
    [draft, setDraft] = useState(""),
    [version, setVersion] = useState(0),
    [loading, setLoading] = useState(true),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [conflict, setConflict] = useState<Note | null>(null),
    [attempt, setAttempt] = useState(0);
  const dirty = useRef(false);
  useEffect(() => {
    const abort = new AbortController();
    setLoading(true);
    setError("");
    setConflict(null);
    dirty.current = false;
    void request<Note[]>(`/meetings/${meetingId}/notes`, {
      signal: abort.signal,
    })
      .then((all) => {
        if (abort.signal.aborted) return;
        setNotes(all);
        const own = all.find((n) => n.authorId === user);
        setDraft(own?.content || "");
        setVersion(own?.version || 0);
      })
      .catch((e) => {
        if (!abort.signal.aborted) setError(e.message);
      })
      .finally(() => {
        if (!abort.signal.aborted) setLoading(false);
      });
    return () => abort.abort();
  }, [meetingId, user, attempt]);
  useEffect(() => {
    const guard = (e: BeforeUnloadEvent) => {
      if (dirty.current) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, []);
  async function save(base = version) {
    if (busy) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const saved = await request<Note>(`/meetings/${meetingId}/notes/me`, {
        method: "PUT",
        body: JSON.stringify({ content: draft, version: base }),
      });
      setVersion(saved.version);
      setNotes((p) => [...p.filter((n) => n.authorId !== user), saved]);
      setConflict(null);
      dirty.current = false;
      setNotice(`已保存 · 版本 ${saved.version}`);
    } catch (e) {
      setError((e as Error).message);
      if (e instanceof ApiError && e.code === "NOTE_VERSION_CONFLICT") {
        try {
          const all = await request<Note[]>(`/meetings/${meetingId}/notes`);
          setNotes(all);
          setConflict(all.find((n) => n.authorId === user) || null);
        } catch {
          setError("读取最新版本失败，本地内容保留，请稍后再保存");
        }
      }
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="manual-notes" aria-label="手动会议笔记">
      {!compact && <h2>手动会议笔记</h2>}
      <p className="muted">
        仅本人可编辑自己的笔记；主持人和实际参会者可查看本场记录。保存后的内容刷新可恢复。
      </p>
      <div className="note-editor">
        {loading ? (
          <p role="status">正在读取笔记…</p>
        ) : (
          <>
            <label>
              我的笔记
              <textarea
                aria-label="我的笔记"
                rows={6}
                maxLength={10000}
                value={draft}
                onChange={(e) => {
                  setDraft(e.target.value);
                  dirty.current = true;
                  setNotice("有未保存的修改");
                }}
                disabled={busy}
              />
            </label>
            <div className="note-savebar">
              <button
                className="button"
                onClick={() => void save()}
                disabled={busy || !!conflict}
              >
                {busy ? "保存中…" : "保存笔记"}
              </button>
              <small> 当前版本 {version}</small>
            </div>
          </>
        )}
      </div>
      <div className="notes-extra">
        {error && (
          <p role="alert" className="error">
            {error}
          </p>
        )}
        {error && loading === false && !dirty.current && !notes.length && (
          <button onClick={() => setAttempt((n) => n + 1)}>重新读取笔记</button>
        )}
        {notice && <p role="status">{notice}</p>}
        {conflict && (
          <section className="note-conflict" aria-label="笔记版本冲突">
            <h3>服务器已有更新（版本 {conflict.version}）</h3>
            <pre>{conflict.content}</pre>
            <p>你的本地内容仍在编辑框中。请比较后选择：</p>
            <button
              onClick={() => {
                setDraft(conflict.content);
                setVersion(conflict.version);
                setConflict(null);
                setError("");
                dirty.current = false;
                setNotice("已加载服务器版本");
              }}
            >
              加载服务器版本，替换本地内容
            </button>
            <button disabled={busy} onClick={() => void save(conflict.version)}>
              保留本地内容，以最新版本重试保存
            </button>
          </section>
        )}
        {notes
          .filter((n) => n.authorId !== user)
          .map((n) => (
            <article className="record-note" key={n.id}>
              <h3>{n.authorName}的笔记</h3>
              <small>
                版本 {n.version} ·{" "}
                {new Date(n.updatedAt).toLocaleString("zh-CN")}
              </small>
              <pre>{n.content || "（空笔记）"}</pre>
            </article>
          ))}
      </div>
    </section>
  );
}
