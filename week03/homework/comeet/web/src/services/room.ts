import { ApiError, request } from "./api";
import type { Meeting, Session } from "./types";
import { useAuth } from "../stores/auth";
export interface Member {
  userId: string;
  sessionId: string;
  displayName: string;
  state: "online" | "reconnecting";
  isHost: boolean;
}
export interface Message {
  id: number;
  clientMessageId: string;
  senderId: string;
  senderName: string;
  content: string;
  createdAt: number;
}
export interface ChatView extends Message {
  delivery: "sent" | "sending" | "failed";
  error?: string;
}
interface Page {
  items: Message[];
  nextCursor: number;
  hasMore: boolean;
}
interface Stored {
  clientRequestId: string;
  sessionId?: string;
  resumeCredential?: string;
  cursor?: number;
}
export interface RoomState {
  sessionId?: string;
  status:
    | "connecting"
    | "syncing"
    | "connected"
    | "reconnecting"
    | "error"
    | "left";
  meeting: Meeting | null;
  members: Member[];
  messages: ChatView[];
  notice: string;
  hasOlder: boolean;
  loadingOlder: boolean;
  conflict: boolean;
  terminalCode?: string;
  controlNotice?: string;
}
interface Frame {
  v: number;
  type: string;
  requestId?: string;
  payload: unknown;
}
const GRACE = 45_000;
export class RoomClient {
  private key: string;
  private record: Stored;
  private ws: WebSocket | null = null;
  private abort: AbortController | null = null;
  private epoch = 0;
  private stopped = false;
  private joined = false;
  private synchronized = false;
  private syncing = false;
  private buffer: Message[] = [];
  private messages = new Map<number, Message>();
  private pending = new Map<string, ChatView>();
  private requests = new Map<string, string>();
  private ackTimers = new Map<string, ReturnType<typeof setTimeout>>();
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private deadline = 0;
  private attempts = 0;
  private lastCode = "";
  private cursor: number;
  private state: RoomState = {
    status: "connecting",
    meeting: null,
    members: [],
    messages: [],
    notice: "正在连接业务服务…",
    hasOlder: false,
    loadingOlder: false,
    conflict: false,
  };
  constructor(
    private meetingId: string,
    private session: Session,
    private publish: (state: RoomState) => void,
  ) {
    this.key = `comeet.room.${session.user.id}.${meetingId}`;
    let stored: Stored | null = null;
    try {
      stored = JSON.parse(sessionStorage.getItem(this.key) || "null");
    } catch {
      /* discard invalid local state */
    }
    this.record =
      stored && typeof stored.clientRequestId === "string"
        ? stored
        : { clientRequestId: crypto.randomUUID() };
    this.cursor =
      Number.isSafeInteger(this.record.cursor) && this.record.cursor! >= 0
        ? this.record.cursor!
        : 0;
    this.save();
  }
  private save() {
    sessionStorage.setItem(this.key, JSON.stringify(this.record));
  }
  private emit(patch: Partial<RoomState> = {}) {
    this.state = {
      ...this.state,
      ...patch,
      messages: [...this.messages.values()]
        .sort((a, b) => a.id - b.id)
        .map((m) => ({ ...m, delivery: "sent" as ChatView["delivery"] }))
        .concat(
          [...this.pending.values()].sort((a, b) => a.createdAt - b.createdAt),
        ),
    };
    if (!this.stopped) this.publish(this.state);
  }
  private api<T>(path: string, body?: unknown) {
    return request<T>(`/meetings/${this.meetingId}${path}`, {
      signal: this.abort?.signal,
      ...(body === undefined
        ? {}
        : { method: "POST", body: JSON.stringify(body) }),
    });
  }
  start() {
    window.addEventListener("offline", this.offline);
    window.addEventListener("online", this.online);
    void this.open();
  }
  private offline = () => {
    if (!this.stopped && this.ws && this.ws.readyState <= WebSocket.OPEN)
      this.ws.close(4000, "network offline");
  };
  private online = () => {
    if (!this.stopped && this.state.status === "reconnecting") {
      if (this.retryTimer) clearTimeout(this.retryTimer);
      this.retryTimer = null;
      void this.open();
    }
  };
  private async open() {
    if (this.stopped) return;
    if (this.deadline && Date.now() >= this.deadline) {
      this.terminal("RECONNECT_EXPIRED", "重连等待已超过 45 秒，请重新加入");
      return;
    }
    const epoch = ++this.epoch;
    this.abort?.abort();
    this.abort = new AbortController();
    this.syncing = true;
    this.buffer = [];
    this.emit({
      status: this.deadline ? "reconnecting" : "connecting",
      notice: this.deadline ? "正在尝试恢复业务连接…" : "正在预留并连接会议…",
      loadingOlder: false,
    });
    try {
      if (this.record.sessionId && this.record.resumeCredential) {
        await this.api("/resume", {
          sessionId: this.record.sessionId,
          resumeCredential: this.record.resumeCredential,
        });
      } else {
        const reservation = await this.api<{
          sessionId: string;
          resumeCredential: string;
        }>("/join", { clientRequestId: this.record.clientRequestId });
        if (this.stopped || epoch !== this.epoch) return;
        this.record = {
          ...this.record,
          sessionId: reservation.sessionId,
          resumeCredential: reservation.resumeCredential,
        };
        this.save();
      }
      if (this.stopped || epoch !== this.epoch) return;
      const ticket = await this.api<{ ticket: string }>("/ws-ticket", {
        sessionId: this.record.sessionId,
      });
      if (this.stopped || epoch !== this.epoch) return;
      const url = new URL("/ws", location.href);
      url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
      url.searchParams.set("ticket", ticket.ticket);
      const ws = new WebSocket(url);
      this.ws = ws;
      ws.onopen = () => {
        if (this.stopped || epoch !== this.epoch) {
          ws.close();
          return;
        }
        this.frame("room.join", {});
      };
      ws.onmessage = (event) => {
        if (this.stopped || epoch !== this.epoch) return;
        try {
          const v: Frame = JSON.parse(event.data);
          if (v.v !== 1) throw Error("不支持的协议");
          this.receive(v, epoch);
        } catch {
          this.terminal("INVALID_RESPONSE", "业务消息格式无效，请重新加入");
        }
      };
      ws.onclose = () => {
        if (this.stopped || epoch !== this.epoch) return;
        this.ws = null;
        this.reconnect("业务连接已断开，正在自动重连…");
      };
      ws.onerror = () => {
        /* close is the single reconnect trigger */
      };
    } catch (e) {
      if (
        this.stopped ||
        epoch !== this.epoch ||
        (e as Error).name === "AbortError"
      )
        return;
      const error = e as ApiError;
      if (error.code === "ALREADY_JOINED") {
        this.lastCode = error.code;
        if (!this.deadline) this.deadline = Date.now() + GRACE;
        this.reconnect(
          "原连接仍在线，等待服务端确认断开；复制标签页不能接管原连接。",
        );
      } else if (
        error.status === 0 ||
        error.status >= 500 ||
        error.code === "TICKET_INVALID"
      ) {
        this.reconnect(error.message);
      } else this.terminal(error.code, error.message);
    }
  }
  private frame(type: string, payload: unknown) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN)
      throw Error("业务连接尚未就绪");
    const requestId = crypto.randomUUID();
    this.ws.send(JSON.stringify({ v: 1, type, requestId, payload }));
    return requestId;
  }
  private receive(event: Frame, epoch: number) {
    if (event.type === "room.snapshot") {
      this.state.sessionId = this.record.sessionId;
      const p = event.payload as {
        meeting: Meeting;
        members: Member[];
        latestMessageId: number;
      };
      this.joined = true;
      this.emit({
        meeting: p.meeting,
        members: p.members,
        status: "syncing",
        notice: "正在补齐聊天记录…",
        conflict: false,
        terminalCode: undefined,
      });
      void this.sync(p.latestMessageId, epoch);
      return;
    }
    if (event.type === "member.muted") {
      this.emit({
        controlNotice: (event.payload as { message: string }).message,
      });
      return;
    }
    if (event.type.startsWith("member.")) {
      const p = event.payload as { members: Member[] };
      if (p.members) this.emit({ members: p.members });
      return;
    }
    if (event.type === "chat.created" || event.type === "chat.ack") {
      const m = event.payload as Message;
      if (this.syncing) {
        if (this.buffer.length >= 1000) {
          this.ws?.close(4001, "resync required");
          return;
        }
        this.buffer.push(m);
      } else {
        this.merge([m]);
        this.cursor = Math.max(this.cursor, m.id);
        this.record.cursor = this.cursor;
        this.save();
        this.emit();
      }
      return;
    }
    if (event.type === "error") {
      const p = event.payload as { code: string; message: string };
      const pending = event.requestId
        ? this.requests.get(event.requestId)
        : undefined;
      if (pending) {
        this.failed(pending, p.message);
        return;
      }
      if (p.code === "SERVER_RESTART") {
        this.emit({ notice: "业务服务重启中，连接恢复后会自动补齐消息。" });
        return;
      }
      if (p.code === "LEFT_MEETING") {
        this.terminal(p.code, "已离开会议");
        return;
      }
      if (p.code === "SESSION_EXPIRED" && p.message.includes("登录")) {
        this.terminal(p.code, p.message);
        useAuth.getState().clear("登录已失效，请重新登录");
        return;
      }
      if (
        [
          "SESSION_EXPIRED",
          "REMOVED_FROM_MEETING",
          "MEETING_ENDED",
          "MEETING_ENDING",
          "ALREADY_JOINED",
        ].includes(p.code)
      ) {
        this.terminal(p.code, p.message);
        return;
      }
      this.emit({ notice: p.message });
    }
  }
  private merge(items: Message[]) {
    for (const m of items) {
      this.messages.set(m.id, m);
      if (m.senderId === this.session.user.id) {
        this.pending.delete(m.clientMessageId);
        const t = this.ackTimers.get(m.clientMessageId);
        if (t) clearTimeout(t);
        this.ackTimers.delete(m.clientMessageId);
        for (const [request, id] of this.requests) {
          if (id === m.clientMessageId) this.requests.delete(request);
        }
      }
    }
  }
  private async sync(high: number, epoch: number) {
    try {
      const cold = !this.synchronized;
      if (cold) {
        const initial = await this.api<Page>("/messages?limit=50");
        if (this.stopped || epoch !== this.epoch) return;
        this.merge(initial.items);
        this.state.hasOlder = initial.hasMore;
        if (this.record.cursor === undefined)
          this.cursor = initial.items.at(-1)?.id || 0;
      }
      if (
        this.cursor > high &&
        this.record.cursor !== undefined &&
        !this.synchronized
      ) {
        this.cursor = 0;
      }
      while (this.cursor < high) {
        const page = await this.api<Page>(
          `/messages?afterId=${this.cursor}&limit=100`,
        );
        if (this.stopped || epoch !== this.epoch) return;
        if (!page.items.length || page.nextCursor <= this.cursor)
          throw Error("消息补拉尚未完成，请重试");
        this.merge(page.items);
        this.cursor = page.nextCursor;
      }
      if (this.stopped || epoch !== this.epoch) return;
      this.merge(this.buffer);
      for (const m of this.buffer) this.cursor = Math.max(this.cursor, m.id);
      this.buffer = [];
      this.syncing = false;
      this.synchronized = true;
      this.deadline = 0;
      this.attempts = 0;
      this.lastCode = "";
      this.record.cursor = this.cursor;
      this.save();
      this.emit({ status: "connected", notice: "业务已连接" });
    } catch (e) {
      if (
        this.stopped ||
        epoch !== this.epoch ||
        (e as Error).name === "AbortError"
      )
        return;
      this.emit({ notice: (e as Error).message });
      this.ws?.close(4001, "history sync failed");
    }
  }
  private reconnect(notice: string) {
    if (this.stopped) return;
    this.abort?.abort();
    if (!this.deadline) this.deadline = Date.now() + GRACE;
    for (const id of this.pending.keys())
      this.failed(id, "连接中断，请在恢复后点击原消息重试");
    if (Date.now() >= this.deadline) {
      this.terminal(
        this.lastCode === "ALREADY_JOINED"
          ? "ALREADY_JOINED"
          : "RECONNECT_EXPIRED",
        this.lastCode === "ALREADY_JOINED"
          ? "原标签页仍在线，请返回原标签页；本标签页不会将其踢下线。"
          : "重连等待已超过 45 秒，请重新加入",
      );
      return;
    }
    this.emit({ status: "reconnecting", notice });
    if (this.retryTimer) clearTimeout(this.retryTimer);
    const base = Math.min(10000, 1000 * 2 ** Math.min(this.attempts++, 4));
    const delay = Math.min(
      10000,
      base + Math.random() * Math.min(base * 0.2, 500),
      this.deadline - Date.now(),
    );
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      void this.open();
    }, delay);
  }
  private terminal(code: string, message: string) {
    this.lastCode = code;
    this.epoch++;
    this.abort?.abort();
    if (this.retryTimer) clearTimeout(this.retryTimer);
    this.retryTimer = null;
    this.ws?.close();
    this.ws = null;
    if (
      [
        "LEFT_MEETING",
        "REMOVED_FROM_MEETING",
        "MEETING_ENDED",
        "MEETING_ENDING",
        "SESSION_EXPIRED",
      ].includes(code)
    )
      this.joined = false;
    for (const id of this.pending.keys())
      this.failed(id, "连接已停止，消息未确认");
    if (code !== "ALREADY_JOINED") sessionStorage.removeItem(this.key);
    this.emit({
      status: code === "LEFT_MEETING" ? "left" : "error",
      notice: message,
      conflict: code === "ALREADY_JOINED",
      terminalCode: code,
    });
  }
  send(content: string) {
    content = content.trim();
    if (!content) throw Error("消息不能为空");
    if (
      [...content].length > 2000 ||
      new TextEncoder().encode(content).length > 8192
    )
      throw Error("消息最多 2000 字符且不超过 8KiB");
    if (this.state.status !== "connected")
      throw Error("业务连接恢复后才能发送消息");
    const id = crypto.randomUUID();
    this.pending.set(id, {
      id: 0,
      clientMessageId: id,
      senderId: this.session.user.id,
      senderName: this.session.user.displayName,
      content,
      createdAt: Date.now(),
      delivery: "sending",
    });
    this.transmit(id);
    return id;
  }
  retry(id: string) {
    if (this.state.status !== "connected") return;
    this.transmit(id);
  }
  private transmit(id: string) {
    const m = this.pending.get(id);
    if (!m) return;
    this.pending.set(id, { ...m, delivery: "sending", error: undefined });
    try {
      const request = this.frame("chat.send", {
        clientMessageId: id,
        content: m.content,
      });
      this.requests.set(request, id);
      const prior = this.ackTimers.get(id);
      if (prior) clearTimeout(prior);
      this.ackTimers.set(
        id,
        setTimeout(() => this.failed(id, "未收到确认，请点击原消息重试"), 8000),
      );
    } catch (e) {
      this.failed(id, (e as Error).message);
    }
    this.emit();
  }
  private failed(id: string, error: string) {
    const m = this.pending.get(id);
    if (!m) return;
    this.pending.set(id, { ...m, delivery: "failed", error });
    const t = this.ackTimers.get(id);
    if (t) clearTimeout(t);
    this.ackTimers.delete(id);
    for (const [key, value] of this.requests) {
      if (value === id) this.requests.delete(key);
    }
    this.emit();
  }
  async older() {
    if (
      this.state.loadingOlder ||
      !this.state.hasOlder ||
      this.state.status !== "connected"
    )
      return;
    const before = Math.min(...this.messages.keys());
    if (!Number.isFinite(before)) return;
    const epoch = this.epoch;
    this.emit({ loadingOlder: true });
    try {
      const page = await this.api<Page>(
        `/messages?beforeId=${before}&limit=50`,
      );
      if (this.stopped || epoch !== this.epoch) return;
      this.merge(page.items);
      this.emit({ hasOlder: page.hasMore });
    } catch (e) {
      if ((e as Error).name !== "AbortError")
        this.emit({ notice: (e as Error).message });
    } finally {
      if (!this.stopped && epoch === this.epoch)
        this.emit({ loadingOlder: false });
    }
  }
  async rejoin() {
    if (this.state.conflict) return;
    this.emit({ sessionId: undefined });
    this.epoch++;
    this.abort?.abort();
    this.ws?.close();
    this.ws = null;
    if (this.record.sessionId) {
      try {
        await request(`/meetings/${this.meetingId}/leave`, {
          method: "POST",
          body: JSON.stringify({ sessionId: this.record.sessionId }),
        });
      } catch {
        /* server grace is the fallback */
      }
      this.record = { clientRequestId: crypto.randomUUID() };
    } else if (this.lastCode === "SESSION_EXPIRED")
      this.record = { clientRequestId: crypto.randomUUID() };
    this.record.cursor = undefined;
    this.cursor = 0;
    this.messages.clear();
    this.pending.clear();
    this.joined = false;
    this.synchronized = false;
    this.deadline = 0;
    this.attempts = 0;
    this.save();
    void this.open();
  }
  async leave() {
    const sessionId = this.record.sessionId;
    const owned = this.joined;
    this.dispose(false);
    if (!sessionId || !owned) return "";
    try {
      await request(`/meetings/${this.meetingId}/leave`, {
        method: "POST",
        body: JSON.stringify({ sessionId }),
      });
      return "";
    } catch {
      return "本地已离开；未能通知服务器，席位将在断线宽限期结束后释放。";
    }
  }
  dispose(notify = true) {
    if (this.stopped) return;
    const owned = this.joined,
      sessionId = this.record.sessionId;
    this.stopped = true;
    this.epoch++;
    this.abort?.abort();
    window.removeEventListener("offline", this.offline);
    window.removeEventListener("online", this.online);
    if (this.retryTimer) clearTimeout(this.retryTimer);
    for (const t of this.ackTimers.values()) clearTimeout(t);
    this.ackTimers.clear();
    this.requests.clear();
    this.messages.clear();
    this.pending.clear();
    this.buffer = [];
    if (owned && this.ws?.readyState === WebSocket.OPEN) {
      try {
        this.frame("room.leave", {});
      } catch {
        /* REST fallback */
      }
    }
    this.ws?.close(1000, "page navigation");
    this.ws = null;
    if (owned) sessionStorage.removeItem(this.key);
    if (notify && owned && sessionId)
      void request(`/meetings/${this.meetingId}/leave`, {
        method: "POST",
        body: JSON.stringify({ sessionId }),
      }).catch(() => undefined);
  }
}
