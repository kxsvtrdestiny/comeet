import type { ReactNode } from "react";
import { Video } from "lucide-react";
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
