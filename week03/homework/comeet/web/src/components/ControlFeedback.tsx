import type { useModeration } from "../services/moderation";
export function ControlFeedback({
  control,
}: {
  control: ReturnType<typeof useModeration>;
}) {
  return (
    <section className="control-feedback" aria-label="会控结果">
      {control.error && <p role="alert">{control.error}</p>}
      {control.notice && <p role="status">{control.notice}</p>}
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
