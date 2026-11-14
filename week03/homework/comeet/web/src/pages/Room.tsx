import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  Monitor,
  Users,
  MessageSquare,
  MicOff,
  VideoOff,
  LogOut,
  Info,
  X,
  Settings,
  Maximize2,
  LayoutGrid,
  Focus,
  Mic,
  Video,
  Copy,
} from "lucide-react";
import { useModeration } from "../services/moderation";
import { ControlFeedback } from "../components/ControlFeedback";
import { ManualNotes } from "../components/ManualNotes";
import { RoomDialog } from "../components/RoomDialog";
import {
  nextSelection,
  sourceKey,
  type ViewSelection,
} from "../services/roomView";
import {
  useMedia,
  MemberMedia,
  AudioSubscriptions,
  MediaControls,
} from "../components/MediaRoom";
import { Track } from "livekit-client";
import { RoomClient, type RoomState } from "../services/room";
import { staticPreview, formatMeetingNo } from "../services/types";
import { useAuth } from "../stores/auth";
const initial: RoomState = {
  status: "connecting",
  meeting: null,
  members: [],
  messages: [],
  notice: "正在连接业务服务…",
  hasOlder: false,
  loadingOlder: false,
  conflict: false,
};
export default function Room() {
  return staticPreview ? <StaticRoom /> : <LiveRoom />;
}
function StaticRoom() {
  return (
    <div className="room">
      <header className="room-header">
        <h1>会议室</h1>
        <Link to="/" className="room-back">
          返回工作台
        </Link>
      </header>
      <div className="room-stage">
        <Monitor size={40} />
        <h2>静态界面预览</h2>
        <p>实时成员、聊天与重连请在 Windows 本机运行后验收。</p>
      </div>
    </div>
  );
}
function LiveRoom() {
  const { id = "" } = useParams();
  const session = useAuth((s) => s.session);
  const nav = useNavigate();
  const client = useRef<RoomClient | null>(null);
  const [state, setState] = useState<RoomState>(initial);
  const [panel, setPanel] = useState<"members" | "chat" | null>(null);
  const [draft, setDraft] = useState("");
  const [error, setError] = useState("");
  const [leaving, setLeaving] = useState(false);
  const [layout, setLayout] = useState<"focus" | "grid">("focus");
  const [selection, setSelection] = useState<ViewSelection>({
    mode: "auto",
    key: null,
  });
  const [focused, setFocused] = useState(false);
  const [dialog, setDialog] = useState<
    "connection" | "devices" | "notes" | null
  >(null);
  const [viewNotice, setViewNotice] = useState("");
  const [unread, setUnread] = useState(0);
  const lastMessage = useRef<number | null>(null);
  const panelElement = useRef<HTMLElement>(null);
  const knownShares = useRef(new Set<string>());
  const media = useMedia(
    id,
    state.sessionId,
    state.status === "error" || state.status === "left" || leaving,
  );
  const log = useRef<HTMLDivElement | null>(null);
  const stickToBottom = useRef(true);
  useEffect(() => {
    if (!session) return;
    setState(initial);
    setSelection({ mode: "auto", key: null });
    setUnread(0);
    lastMessage.current = null;
    setPanel(null);
    knownShares.current.clear();
    const c = new RoomClient(id, session, setState);
    client.current = c;
    c.start();
    return () => {
      c.dispose();
      if (client.current === c) client.current = null;
    };
  }, [id, session]);
  const members = state.members.map((m) => {
    const local = m.sessionId === state.sessionId;
    const participant = local
      ? media.state.room?.localParticipant
      : media.state.room?.remoteParticipants.get(m.sessionId);
    const camera = participant?.getTrackPublication(Track.Source.Camera);
    const mic = participant?.getTrackPublication(Track.Source.Microphone);
    return {
      key: sourceKey(m.sessionId, "camera"),
      participantIdentity: m.sessionId,
      source: "camera" as const,
      local,
      available: !!camera?.track && !camera.isMuted,
      microphone: !!mic?.track && !mic.isMuted,
      participant,
      member: m,
    };
  });
  const sources = members.flatMap((m) => {
    const share = m.participant?.getTrackPublication(Track.Source.ScreenShare);
    const available =
      media.state.status === "connected" &&
      !!share?.track &&
      !share.isMuted &&
      share.track.mediaStreamTrack.readyState === "live";
    const recovering =
      media.state.status === "reconnecting" &&
      knownShares.current.has(m.participantIdentity);
    return available || recovering
      ? [
          m,
          {
            ...m,
            key: sourceKey(m.member.sessionId, "screenShare"),
            source: "screenShare" as const,
            available,
            recovering,
          },
        ]
      : [m];
  });
  useEffect(() => {
    if (media.state.status === "connected")
      knownShares.current = new Set(
        sources
          .filter((s) => s.source === "screenShare" && s.available)
          .map((s) => s.participantIdentity),
      );
    else if (media.state.status !== "reconnecting") knownShares.current.clear();
  }, [media.state.status, media.state.revision]);
  const isHost = state.meeting?.hostUserId === session?.user.id && !!session;
  const control = useModeration(id, isHost, () => {
    media.stop();
    nav("/", { replace: true, state: { leaveNotice: "会议已结束" } });
  });
  useEffect(() => {
    if (
      state.terminalCode === "REMOVED_FROM_MEETING" ||
      state.terminalCode === "MEETING_ENDED" ||
      (state.terminalCode === "MEETING_ENDING" && state.meeting && !isHost)
    ) {
      media.stop();
      nav("/", { replace: true, state: { leaveNotice: state.notice } });
    }
  }, [state.terminalCode, state.meeting, isHost, nav]);
  const resolved = nextSelection(selection, sources);
  const main = sources.find((s) => s.key === resolved.key);
  // Only membership/source loss clears a selection; a muted camera remains selectable.
  useEffect(() => {
    if (resolved.key !== selection.key || resolved.mode !== selection.mode) {
      if (selection.mode === "manual")
        setViewNotice(
          selection.key &&
            sources.some(
              (s) => s.participantIdentity === JSON.parse(selection.key!)[0],
            )
            ? "所选共享已停止，已恢复自动选择"
            : "所选成员已离会，已恢复自动选择",
        );
      setSelection(resolved);
    }
  }, [resolved.key, resolved.mode, selection.key, selection.mode]);
  useEffect(() => {
    if (!viewNotice) return;
    const timer = setTimeout(() => setViewNotice(""), 4500);
    return () => clearTimeout(timer);
  }, [viewNotice]);
  useEffect(() => {
    const highest = Math.max(0, ...state.messages.map((m) => m.id));
    const fresh = state.messages.filter(
      (m) =>
        lastMessage.current !== null &&
        m.id > lastMessage.current &&
        m.senderId !== session?.user.id,
    );
    if (state.status === "connected")
      lastMessage.current = Math.max(lastMessage.current || 0, highest);
    if (panel === "chat") setUnread(0);
    else if (state.status === "connected" && fresh.length)
      setUnread((n) => n + fresh.length);
    if (panel === "chat" && log.current && stickToBottom.current)
      log.current.scrollTop = log.current.scrollHeight;
  }, [state.messages, state.status, panel, session?.user.id]);
  // On small screens the shared panel is a modal drawer with trapped focus and Escape.
  useEffect(() => {
    if (!panel || !window.matchMedia("(max-width: 767px)").matches) return;
    const previous = document.activeElement as HTMLElement | null;
    const el = panelElement.current!;
    el.focus();
    const keydown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        setPanel(null);
      }
      if (e.key !== "Tab") return;
      const items = Array.from(
        el.querySelectorAll<HTMLElement>(
          'button:not(:disabled), textarea:not(:disabled), [tabindex="0"]',
        ),
      );
      const first = items[0],
        last = items.at(-1);
      if (
        e.shiftKey &&
        (document.activeElement === first || document.activeElement === el)
      ) {
        e.preventDefault();
        last?.focus();
      } else if (
        !e.shiftKey &&
        (document.activeElement === last || document.activeElement === el)
      ) {
        e.preventDefault();
        first?.focus();
      }
    };
    el.addEventListener("keydown", keydown);
    return () => {
      el.removeEventListener("keydown", keydown);
      previous?.focus();
    };
  }, [panel]);
  function togglePanel(next: "members" | "chat") {
    setFocused(false);
    setPanel((p) => (p === next ? null : next));
  }
  function select(key: string) {
    setSelection({ mode: "manual", key });
    setLayout("focus");
  }
  async function leave() {
    if (leaving) return;
    media.stop();
    setLeaving(true);
    const notice = await client.current?.leave();
    nav("/", { replace: true, state: { leaveNotice: notice || "" } });
  }
  function send(e: FormEvent) {
    e.preventDefault();
    try {
      client.current?.send(draft);
      stickToBottom.current = true;
      setDraft("");
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }
  const terminal = state.status === "error" || state.status === "left";
  const mediaFailed =
    media.state.status === "error" || media.state.status === "unconfigured";
  function tile(s: (typeof sources)[number], thumbnail = false) {
    return (
      <article
        className={`member-tile ${s.source === "screenShare" ? "screen-tile" : ""} ${thumbnail ? "thumbnail" : ""} ${s.key === main?.key ? "selected" : ""}`}
        key={s.key}
      >
        {s.member.state === "online" && media.state.status === "connected" && (
          <MemberMedia
            participant={s.participant}
            local={s.local}
            screen={s.source === "screenShare"}
          />
        )}
        {(!s.available ||
          s.member.state !== "online" ||
          media.state.status !== "connected") && (
          <div className="camera-placeholder">
            <div className="avatar">{s.member.displayName.slice(0, 1)}</div>
            <p>
              {s.member.state === "reconnecting"
                ? "成员重连中…"
                : media.state.status === "reconnecting"
                  ? "音视频重连中…"
                  : media.state.status === "connecting"
                    ? "正在连接音视频…"
                    : "摄像头已关闭"}
            </p>
          </div>
        )}
        <div className="tile-caption">
          <strong>
            {s.member.displayName}
            {s.source === "screenShare" ? "的共享" : ""}
          </strong>
          {s.local && <span>本人</span>}
          {s.member.isHost && <span>主持人</span>}
          <span aria-label={s.microphone ? "麦克风开启" : "麦克风关闭"}>
            <span className="sr-only">
              {s.microphone ? "麦克风开启" : "麦克风关闭"}
            </span>
            {s.microphone ? <Mic size={14} /> : <MicOff size={14} />}
          </span>
          {s.key === main?.key && <span>主画面</span>}
        </div>
        {(thumbnail || layout === "grid") && (
          <button
            className="tile-select"
            aria-label={`设为主画面：${s.member.displayName}${s.source === "screenShare" ? "的共享" : ""}`}
            onClick={() => select(s.key)}
          >
            <Maximize2 size={14} />
            设为主画面
          </button>
        )}
      </article>
    );
  }
  return (
    <div className={`room live-room ${focused ? "is-focused" : ""}`}>
      <header className="room-header">
        <div className="room-heading">
          <h1 title={state.meeting?.title}>
            {state.meeting?.title || "会议室"}
          </h1>
          {state.meeting && (
            <button
              className="meeting-number"
              onClick={() =>
                void navigator.clipboard
                  .writeText(state.meeting!.meetingNo)
                  .then(() => setViewNotice("会议号已复制"))
                  .catch(() => setViewNotice("复制失败，请手动复制会议号"))
              }
              title="复制会议号"
            >
              <Copy size={12} />
              {formatMeetingNo(state.meeting.meetingNo)}
            </button>
          )}
        </div>
        <div className="room-actions">
          <button
            aria-pressed={layout === "focus"}
            onClick={() => setLayout("focus")}
            title="聚焦布局"
          >
            <Focus size={17} />
            <span>聚焦</span>
          </button>
          <button
            aria-pressed={layout === "grid"}
            onClick={() => setLayout("grid")}
            title="宫格布局"
          >
            <LayoutGrid size={17} />
            <span>宫格</span>
          </button>
          <button
            aria-label="连接详情"
            title="连接详情"
            onClick={() => setDialog("connection")}
          >
            <Info size={18} />
          </button>
          <button
            aria-pressed={focused}
            onClick={() => {
              setFocused(!focused);
              setPanel(null);
            }}
            title={focused ? "退出专注" : "专注观看"}
          >
            <Maximize2 size={17} />
            <span>{focused ? "退出专注" : "专注观看"}</span>
          </button>
        </div>
      </header>
      <div className="room-workspace">
        <div className="room-alerts">
          {terminal || state.conflict ? (
            <section
              className="room-notice"
              data-channel="business"
              role="alert"
            >
              <span>{state.notice}</span>
              {!state.conflict &&
                state.status !== "left" &&
                ![
                  "MEETING_ENDING",
                  "MEETING_ENDED",
                  "REMOVED_FROM_MEETING",
                ].includes(state.terminalCode || "") && (
                  <button onClick={() => client.current?.rejoin()}>
                    重新加入
                  </button>
                )}
              {state.conflict && <span>请返回原标签页，或关闭本标签页。</span>}
            </section>
          ) : (
            state.status !== "connected" && (
              <p role="status" className="room-notice" data-channel="business">
                {state.status === "reconnecting"
                  ? /原连接|原标签页/.test(state.notice)
                    ? state.notice
                    : "聊天与成员同步暂时中断，正在重连"
                  : "正在连接会议…"}
              </p>
            )
          )}
          {!terminal &&
            (mediaFailed || media.state.status === "reconnecting") && (
              <p role="status" className="room-notice">
                {media.state.status === "reconnecting"
                  ? "音视频连接中断，正在重连"
                  : media.state.status === "unconfigured"
                    ? "音视频尚未配置"
                    : "音视频连接失败"}
                {state.status === "connected" ? "；聊天仍可用" : ""}
                {mediaFailed && (
                  <button onClick={media.retry}>重试音视频连接</button>
                )}
              </p>
            )}
          {media.state.error && (
            <p role="alert" className="room-notice">
              {media.state.error}
              <button onClick={() => setDialog("devices")}>检查设备</button>
            </p>
          )}
          {media.state.playbackBlocked &&
            media.state.status === "connected" && (
              <button
                className="audio-unlock"
                onClick={() => void media.client?.startAudio()}
              >
                播放会议声音
              </button>
            )}
          {state.controlNotice && (
            <p className="view-notice" role="status">
              {state.controlNotice}
            </p>
          )}
          {isHost && <ControlFeedback control={control} />}
          {viewNotice && (
            <p className="view-notice" role="status">
              {viewNotice}
            </p>
          )}
        </div>
        <AudioSubscriptions state={media.state} />
        <div className={`room-body ${panel ? "has-panel" : ""}`}>
          <section className={`room-stage ${layout}`} aria-label="参会成员">
            <div className="view-mode">
              <span>
                {selection.mode === "manual" ? "已固定主画面" : "自动选择"}
              </span>
              <button
                onClick={() =>
                  setSelection(
                    nextSelection({ mode: "auto", key: null }, sources),
                  )
                }
              >
                恢复自动选择
              </button>
            </div>
            {sources.length ? (
              layout === "grid" && !focused ? (
                <div className="member-grid" data-count={sources.length}>
                  {sources.map((s) => tile(s))}
                </div>
              ) : (
                <>
                  <div className="main-view">{main && tile(main)}</div>
                  {!focused && sources.length > 1 && (
                    <div className="thumbnail-strip" aria-label="选择主画面">
                      {sources
                        .filter((s) => s.key !== main?.key)
                        .map((s) => tile(s, true))}
                    </div>
                  )}
                </>
              )
            ) : (
              <div className="empty-media">
                <Monitor size={40} />
                <h2>{terminal ? "暂未加入会议" : "正在连接会议…"}</h2>
              </div>
            )}
          </section>
          {panel && (
            <>
              <button
                className="drawer-backdrop"
                aria-label="关闭侧栏遮罩"
                onClick={() => setPanel(null)}
              />
              <aside
                className="room-panel"
                ref={panelElement}
                tabIndex={-1}
                aria-label="成员与聊天面板"
              >
                <div className="tabs">
                  <button
                    onClick={() => setPanel("members")}
                    aria-pressed={panel === "members"}
                  >
                    <Users size={16} />
                    成员（{state.members.length}）
                  </button>
                  <button
                    onClick={() => setPanel("chat")}
                    aria-pressed={panel === "chat"}
                  >
                    <MessageSquare size={16} />
                    聊天
                  </button>
                  <button aria-label="关闭面板" onClick={() => setPanel(null)}>
                    <X size={18} />
                  </button>
                </div>
                {panel === "members" ? (
                  <ul className="member-list">
                    {members.map((s) => (
                      <li key={s.key}>
                        <strong>{s.member.displayName}</strong>
                        {s.local && <span>本人</span>}
                        {s.member.isHost && <span>主持人</span>}
                        <small>
                          {s.member.state === "online" ? "在线" : "重连中"}
                        </small>
                        <small>
                          {s.microphone ? "麦克风开" : "麦克风关"} ·{" "}
                          {s.available ? "摄像头开" : "摄像头关"}
                        </small>
                        <button
                          onClick={() => select(s.key)}
                          aria-pressed={main?.key === s.key}
                        >
                          设为主画面：{s.member.displayName}
                        </button>
                        {sources
                          .filter(
                            (v) =>
                              v.participantIdentity === s.participantIdentity &&
                              v.source === "screenShare",
                          )
                          .map((v) => (
                            <button key={v.key} onClick={() => select(v.key)}>
                              查看{s.member.displayName}的共享
                            </button>
                          ))}
                        {isHost && !s.member.isHost && (
                          <>
                            <button
                              disabled={control.busy}
                              onClick={() =>
                                void control.run("mute", s.member.userId)
                              }
                            >
                              静音：{s.member.displayName}
                            </button>
                            <button
                              disabled={control.busy}
                              onClick={() =>
                                control.confirm("remove", s.member.userId)
                              }
                            >
                              移出：{s.member.displayName}
                            </button>
                          </>
                        )}
                      </li>
                    ))}
                    {!sources.length && <li>尚未取得成员快照</li>}
                  </ul>
                ) : (
                  <>
                    <div
                      className="chat-log"
                      ref={log}
                      onScroll={(e) => {
                        const el = e.currentTarget;
                        stickToBottom.current =
                          el.scrollHeight - el.scrollTop - el.clientHeight < 48;
                      }}
                      aria-label="聊天记录"
                      aria-live="polite"
                    >
                      {state.hasOlder && (
                        <button
                          className="text-button"
                          disabled={
                            state.loadingOlder || state.status !== "connected"
                          }
                          onClick={() => {
                            stickToBottom.current = false;
                            void client.current?.older();
                          }}
                        >
                          {state.loadingOlder ? "正在加载…" : "加载更早消息"}
                        </button>
                      )}
                      {!state.messages.length && (
                        <p className="chat-empty">
                          {state.status === "connected"
                            ? "暂无聊天消息"
                            : "等待聊天记录同步…"}
                        </p>
                      )}
                      {state.messages.map((m) => (
                        <article
                          className={`chat-message ${m.senderId === session?.user.id ? "mine" : ""}`}
                          key={m.id > 0 ? String(m.id) : m.clientMessageId}
                          data-client-message-id={m.clientMessageId}
                          data-message-id={m.id || undefined}
                        >
                          <header>
                            <strong>{m.senderName}</strong>
                            <time>
                              {new Date(m.createdAt).toLocaleTimeString(
                                "zh-CN",
                              )}
                            </time>
                          </header>
                          <p>{m.content}</p>
                          {m.senderId === session?.user.id && (
                            <small>
                              {m.delivery === "sent" ? (
                                <span className="sr-only">已发送</span>
                              ) : m.delivery === "sending" ? (
                                "发送中…"
                              ) : (
                                <>
                                  <span>{m.error || "发送失败"}</span>
                                  <button
                                    disabled={state.status !== "connected"}
                                    onClick={() =>
                                      client.current?.retry(m.clientMessageId)
                                    }
                                  >
                                    重试此消息
                                  </button>
                                </>
                              )}
                            </small>
                          )}
                        </article>
                      ))}
                    </div>
                    <form className="chat-composer" onSubmit={send}>
                      <label className="sr-only" htmlFor="chat-input">
                        消息
                      </label>
                      <textarea
                        id="chat-input"
                        value={draft}
                        onChange={(e) => setDraft(e.target.value)}
                        maxLength={4000}
                        disabled={state.status !== "connected"}
                        placeholder={
                          state.status === "connected"
                            ? "输入消息（最多 2000 字符）"
                            : "业务连接恢复后可发送"
                        }
                        rows={2}
                      />
                      {error && (
                        <p role="alert" className="error">
                          {error}
                        </p>
                      )}
                      <button
                        className="button"
                        disabled={state.status !== "connected" || !draft.trim()}
                      >
                        发送
                      </button>
                    </form>
                  </>
                )}
              </aside>
            </>
          )}
        </div>
      </div>
      <footer className="toolbar">
        <button onClick={() => setDialog("notes")}>会议笔记</button>
        <button
          disabled={
            media.state.status !== "connected" || media.state.sharingBusy
          }
          onClick={() => {
            if (media.state.screenSharing || media.state.sharingCleanupPending)
              void media.client?.stopScreenShare();
            else void media.client?.startScreenShare();
          }}
        >
          <Monitor size={18} />
          {media.state.sharingCleanupPending
            ? "重试停止共享"
            : media.state.screenSharing
              ? "停止共享"
              : media.state.sharingBusy
                ? "正在选择共享…"
                : "共享屏幕"}
        </button>
        {isHost && (
          <button
            disabled={control.busy}
            onClick={() => control.confirm("end")}
          >
            结束会议
          </button>
        )}
        {state.terminalCode === "MEETING_ENDING" && (
          <Link to={`/meetings/${id}`}>会议详情/重试结束</Link>
        )}
        <button
          disabled={media.state.status !== "connected" || media.state.busy}
          onClick={() =>
            void media.client?.toggle("microphone", !media.state.microphone)
          }
        >
          {media.state.microphone ? <Mic size={18} /> : <MicOff size={18} />}
          <span>{media.state.microphone ? "关闭麦克风" : "开启麦克风"}</span>
        </button>
        <button
          disabled={media.state.status !== "connected" || media.state.busy}
          onClick={() =>
            void media.client?.toggle("camera", !media.state.camera)
          }
        >
          {media.state.camera ? <Video size={18} /> : <VideoOff size={18} />}
          <span>{media.state.camera ? "关闭摄像头" : "开启摄像头"}</span>
        </button>
        <button
          aria-pressed={panel === "members"}
          onClick={() => togglePanel("members")}
        >
          <Users size={18} />
          成员（{state.members.length}）
        </button>
        <button
          aria-pressed={panel === "chat"}
          onClick={() => togglePanel("chat")}
        >
          <MessageSquare size={18} />
          聊天{unread > 0 && <span className="unread">（{unread} 未读）</span>}
        </button>
        <button onClick={() => setDialog("devices")}>
          <Settings size={18} />
          设备设置
        </button>
        <button className="leave-button" onClick={leave} disabled={leaving}>
          <LogOut size={18} />
          {leaving ? "离开中…" : "离开会议"}
        </button>
      </footer>
      {dialog && (
        <RoomDialog
          title={
            dialog === "connection"
              ? "连接详情"
              : dialog === "notes"
                ? "会议笔记"
                : "设备设置"
          }
          close={() => setDialog(null)}
        >
          {dialog === "notes" ? (
            <ManualNotes meetingId={id} />
          ) : dialog === "connection" ? (
            <>
              <p>
                聊天与成员连接：
                {state.status === "connected" ? "正常" : state.notice}
              </p>
              <p className={`connection-status ${state.status}`}>
                技术状态：{state.status} · {state.notice}
              </p>
              <p>
                音视频连接：
                {media.state.status === "connected"
                  ? "正常"
                  : media.state.notice}
              </p>
              <p>技术状态：{media.state.status}</p>
            </>
          ) : (
            <MediaControls
              state={media.state}
              client={media.client}
              retry={media.retry}
            />
          )}
        </RoomDialog>
      )}
    </div>
  );
}
