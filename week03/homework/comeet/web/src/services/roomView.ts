// UI identity survives camera mute, track replacement and short reconnections.
// Screen sharing is a future source type, not a capture/publish implementation.
export type ViewSource = {
  key: string;
  participantIdentity: string;
  source: "camera" | "screenShare";
  local: boolean;
  available: boolean;
};
export type ViewSelection = { mode: "auto" | "manual"; key: string | null };
export const sourceKey = (identity: string, source: ViewSource["source"]) =>
  JSON.stringify([identity, source]);
export function nextSelection(
  current: ViewSelection,
  sources: ViewSource[],
): ViewSelection {
  const valid = sources.filter((s) => s.source === "camera" || s.available);
  const previous = valid.find((s) => s.key === current.key);
  if (current.mode === "manual" && previous) return current;
  const shared = valid.find((s) => s.source === "screenShare");
  const preferred =
    shared ||
    previous ||
    valid.find((s) => !s.local && s.available) ||
    valid.find((s) => s.local && s.available) ||
    valid[0];
  return { mode: "auto", key: preferred?.key || null };
}
