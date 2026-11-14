import { useEffect, useRef, useState } from "react";
import { X } from "lucide-react";
import type { PendingControl, useModeration } from "../services/moderation";
type Toast = {
  id: number;
  kind: "success" | "error";
  text: string;
  retry?: PendingControl;
};
export function RoomNotifications({
  control,
  externalNotice,
  externalId,
  panel,
}: {
  control: ReturnType<typeof useModeration>;
  externalNotice?: string;
  externalId?: string;
  panel: string | null;
}) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    setToasts((p) =>
      p.filter(
        (t) => !t.retry || control.pending.some((op) => op.id === t.retry!.id),
      ),
    );
  }, [control.pending]);
  useEffect(() => {
    if (control.feedback) setToasts((p) => [...p.slice(-2), control.feedback!]);
  }, [control.feedback]);
  useEffect(() => {
    if (externalNotice)
      setToasts((p) => [
        ...p.slice(-2),
        { id: Date.now(), kind: "success", text: externalNotice },
      ]);
  }, [externalNotice, externalId]);
  useEffect(() => {
    const timers = toasts
      .filter((t) => t.kind === "success")
      .map((t) =>
        setTimeout(
          () => setToasts((p) => p.filter((v) => v.id !== t.id)),
          5000,
        ),
      );
    return () => timers.forEach(clearTimeout);
  }, [toasts]);
  useEffect(() => {
    const root = box.current?.closest<HTMLElement>(".live-room");
    if (!root) return;
    const update = () => {
      const bounds = root.getBoundingClientRect(),
        footer = root.querySelector(".toolbar")?.getBoundingClientRect();
      let top = footer?.top || bounds.bottom;
      const mobile = window.matchMedia("(max-width: 767px)").matches;
      if (mobile) {
        const protectedArea = root.querySelector(
          panel === "notes"
            ? ".note-editor"
            : panel === "chat"
              ? ".chat-composer"
              : ".toolbar",
        );
        if (protectedArea)
          top = Math.min(top, protectedArea.getBoundingClientRect().top);
      }
      const side = root.querySelector(".room-panel")?.getBoundingClientRect();
      root.style.setProperty(
        "--toast-bottom",
        `${Math.max(12, bounds.bottom - top + 8)}px`,
      );
      root.style.setProperty(
        "--toast-right",
        `${!mobile && panel && side ? bounds.right - side.left + 12 : 12}px`,
      );
    };
    const observer = new ResizeObserver(update);
    observer.observe(root);
    root
      .querySelectorAll(".toolbar,.room-panel,.note-editor,.chat-composer")
      .forEach((el) => observer.observe(el));
    window.addEventListener("resize", update);
    update();
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", update);
    };
  }, [panel, toasts.length]);
  return (
    <div className="room-toasts" ref={box} aria-label="操作通知">
      {toasts.map((t) => (
        <section
          className={`room-toast ${t.kind}`}
          key={t.id}
          role={t.kind === "error" ? "alert" : "status"}
          aria-atomic="true"
        >
          <p>{t.text}</p>
          <div>
            {t.retry && (
              <button
                disabled={control.busy}
                onClick={() =>
                  void control.run(
                    t.retry!.action,
                    t.retry!.targetUserId,
                    t.retry!.id,
                  )
                }
              >
                重试此操作
              </button>
            )}
            <button
              aria-label="关闭通知"
              onClick={() => setToasts((p) => p.filter((v) => v.id !== t.id))}
            >
              <X size={16} />
              关闭
            </button>
          </div>
        </section>
      ))}
    </div>
  );
}
