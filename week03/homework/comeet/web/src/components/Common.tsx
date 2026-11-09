import type { ReactNode } from "react";
import { Video } from "lucide-react";
export function Disabled({
  children,
  stage = 2,
}: {
  children: ReactNode;
  stage?: number;
}) {
  return (
    <button className="button" disabled title={`第 ${stage} 阶段开放`}>
      {children}
      <small>第 {stage} 阶段</small>
    </button>
  );
}
export function Empty({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <div className="empty">
      <Video size={28} />
      <h3>{title}</h3>
      <p>{children}</p>
    </div>
  );
}
