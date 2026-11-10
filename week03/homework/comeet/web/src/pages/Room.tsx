import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  Monitor,
  Users,
  MessageSquare,
  MicOff,
  VideoOff,
  LogOut,
} from "lucide-react";
import { Disabled } from "../components/Common";
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
  const [state, setState] = useState<RoomState>(initial),
    [panel, setPanel] = useState<"members" | "chat">("chat"),
    [draft, setDraft] = useState(""),
    [error, setError] = useState(""),
    [leaving, setLeaving] = useState(false);
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
    const c = new RoomClient(id, session, setState);
    client.current = c;
    c.start();
    return () => {
      c.dispose();
      if (client.current === c) client.current = null;
    };
  }, [id, session]);
  useEffect(() => {
    if (panel === "chat" && log.current && stickToBottom.current)
      log.current.scrollTop = log.current.scrollHeight;
  }, [state.messages.length, panel]);
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
  return (
    <div className="room">
      <header className="room-header">
        <div>
          <h1>{state.meeting?.title || "会议室"}</h1>
          {state.meeting && (
            <p>会议号：{formatMeetingNo(state.meeting.meetingNo)}</p>
          )}
          <p
            role="status"
            aria-label="业务连接"
            className={`connection-status ${state.status}`}
          >
            {state.notice}
          </p>
        </div>
        <button
          className="room-back text-button"
          onClick={leave}
          disabled={leaving}
        >
          {leaving ? "离开中…" : "离开会议，返回工作台"}
        </button>
      </header>
      {(state.status === "error" || state.status === "left") && (
        <section className="room-notice" role="alert">
          <p>{state.notice}</p>
          {!state.conflict && state.status !== "left" && (
            <button className="button" onClick={() => client.current?.rejoin()}>
              重新加入
            </button>
          )}
          {state.conflict && (
            <p>请返回原标签页，或关闭本标签页。不会接管原连接。</p>
          )}
        </section>
      )}
      <p role="status" aria-label="媒体连接" className="media-status">
        {media.state.notice}
      </p>
      <AudioSubscriptions state={media.state} />
      <div className="room-body">
        <section className="room-stage" aria-label="参会成员">
          <div className="member-grid">
            {state.members.map((m) => {
              const local = m.sessionId === state.sessionId;
              const participant = local
                ? media.state.room?.localParticipant
                : media.state.room?.remoteParticipants.get(m.sessionId);
              const video = participant?.getTrackPublication(
                Track.Source.Camera,
              );
              const mic = participant?.getTrackPublication(
                Track.Source.Microphone,
              );
              return (
                <article className="member-tile" key={m.sessionId}>
                  <MemberMedia participant={participant} local={local} />
                  {(!video?.track || video.isMuted) && (
                    <div className="avatar">{m.displayName.slice(0, 1)}</div>
                  )}
                  <h2>{m.displayName}</h2>
                  <div className="member-badges">
                    {m.userId === session?.user.id && (
                      <span className="tag">本人</span>
                    )}
                    {m.isHost && <span className="tag">主持人</span>}
                    <span className="tag">
                      {m.state === "online" ? "在线" : "重连中"}
                    </span>
                  </div>
                  <p>
                    {participant
                      ? `${mic?.track && !mic.isMuted ? "麦克风开启" : "麦克风关闭"} · ${video?.track && !video.isMuted ? "摄像头开启" : "摄像头关闭"}`
                      : "仅文字参会"}
                  </p>
                </article>
              );
            })}
          </div>
          {!state.members.length && (
            <>
              <Monitor size={40} />
              <h2>
                {state.status === "error" ? "暂未加入会议" : "正在连接会议"}
              </h2>
            </>
          )}
        </section>
        <aside className="room-panel">
          <details className="room-media-settings">
            <summary>音视频设置</summary>
            <MediaControls
              state={media.state}
              client={media.client}
              retry={media.retry}
            />
          </details>
          {media.state.playbackBlocked &&
            media.state.status === "connected" && (
              <button
                className="button"
                onClick={() => void media.client?.startAudio()}
              >
                点击启用声音
              </button>
            )}
          <div className="tabs">
            <button
              onClick={() => setPanel("members")}
              aria-pressed={panel === "members"}
            >
              <Users size={17} />
              成员（{state.members.length}）
            </button>
            <button
              onClick={() => setPanel("chat")}
              aria-pressed={panel === "chat"}
            >
              <MessageSquare size={17} />
              聊天
            </button>
          </div>
          {panel === "members" ? (
            <ul className="member-list">
              {state.members.map((m) => (
                <li key={m.sessionId}>
                  <strong>{m.displayName}</strong>
                  {m.userId === session?.user.id && <span>本人</span>}
                  {m.isHost && <span>主持人</span>}
                  <small>{m.state === "online" ? "在线" : "重连中"}</small>
                </li>
              ))}
              {!state.members.length && <li>尚未取得成员快照</li>}
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
                        {new Date(m.createdAt).toLocaleTimeString("zh-CN")}
                      </time>
                    </header>
                    <p>{m.content}</p>
                    {m.senderId === session?.user.id && (
                      <small>
                        {m.delivery === "sent" ? (
                          "已发送"
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
                  rows={3}
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
      </div>
      <footer className="toolbar">
        <button
          className="text-button"
          disabled={media.state.status !== "connected" || media.state.busy}
          onClick={() =>
            void media.client?.toggle("microphone", !media.state.microphone)
          }
        >
          <MicOff size={18} />
          {media.state.microphone ? "关闭麦克风" : "开启麦克风"}
        </button>
        <button
          className="text-button"
          disabled={media.state.status !== "connected" || media.state.busy}
          onClick={() =>
            void media.client?.toggle("camera", !media.state.camera)
          }
        >
          <VideoOff size={18} />
          {media.state.camera ? "关闭摄像头" : "开启摄像头"}
        </button>
        <Disabled stage={5}>
          <Monitor size={18} />
          共享屏幕
        </Disabled>
        <button
          className="button leave-button"
          onClick={leave}
          disabled={leaving}
        >
          <LogOut size={18} />
          {leaving ? "离开中…" : "离开会议"}
        </button>
      </footer>
    </div>
  );
}
