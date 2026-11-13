import { useEffect, useState } from "react";
import { post, request } from "./api";
export type ControlAction = "mute" | "remove" | "end";
export interface PendingControl {
  id: string;
  action: ControlAction;
  targetUserId: string;
}
export function useModeration(
  meetingId: string,
  enabled: boolean,
  ended: () => void,
) {
  const [pending, setPending] = useState<PendingControl[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  useEffect(() => {
    if (!enabled) return;
    const abort = new AbortController();
    void request<PendingControl[]>(
      `/meetings/${encodeURIComponent(meetingId)}/controls`,
      { signal: abort.signal },
    )
      .then(setPending)
      .catch((e) => {
        if (!abort.signal.aborted) setError((e as Error).message);
      });
    return () => abort.abort();
  }, [meetingId, enabled]);
  async function run(
    action: ControlAction,
    targetUserId = "",
    operationId: string = crypto.randomUUID(),
  ) {
    if (busy || !enabled) return;
    if (!operationId) return;
    setBusy(true);
    setError("");
    setNotice("");
    const op = { id: operationId, action, targetUserId };
    const path =
      action === "end"
        ? `/meetings/${meetingId}/end`
        : `/meetings/${meetingId}/members/${targetUserId}/${action}`;
    try {
      await post(path, { requestId: operationId });
      setPending((p) =>
        p.filter(
          (v) =>
            v.id !== operationId &&
            !(
              action !== "mute" &&
              v.action === action &&
              v.targetUserId === targetUserId
            ),
        ),
      );
      setNotice(
        action === "mute"
          ? "已执行一次静音，成员仍可自行开麦"
          : action === "remove"
            ? "成员已移出，媒体清理完成"
            : "会议已结束",
      );
      if (action === "end") ended();
    } catch (e) {
      setError((e as Error).message);
      setPending((p) => (p.some((v) => v.id === op.id) ? p : [...p, op]));
    } finally {
      setBusy(false);
    }
  }
  function confirm(action: "remove" | "end", target = "") {
    if (
      window.confirm(
        action === "end"
          ? "结束所有人的会议？结束后所有人退出且不可重新加入。"
          : "移出该成员？该账号将无法重新加入本场会议。",
      )
    )
      void run(action, target);
  }
  return { pending, busy, error, notice, run, confirm };
}
