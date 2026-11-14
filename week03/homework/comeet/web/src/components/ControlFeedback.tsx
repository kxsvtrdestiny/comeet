import type { useModeration } from "../services/moderation";
export function ControlFeedback({
  control,
  pendingOnly = false,
}: {
  control: ReturnType<typeof useModeration>;
  pendingOnly?: boolean;
}) {
  return (
    <section className="control-feedback" aria-label="会控结果">
      {!pendingOnly && control.error && <p role="alert">{control.error}</p>}
      {!pendingOnly && control.notice && <p role="status">{control.notice}</p>}
      {control.pending.map((op) => (
        <div key={op.id}>
          <span>
            {op.action === "end"
              ? "结束会议"
              : op.action === "remove"
                ? "移出成员"
                : "静音成员"}
            操作未完成或结果待确认
          </span>
          {op.message && <p>{op.message}</p>}
          <button
            disabled={control.busy}
            onClick={() => void control.run(op.action, op.targetUserId, op.id)}
          >
            重试
            {op.action === "end"
              ? "结束会议"
              : op.action === "remove"
                ? "移出成员"
                : "静音成员"}
          </button>
        </div>
      ))}
    </section>
  );
}
