// UI identity survives camera mute, track replacement and short reconnections.
// Camera and screen share are independent sources for the same participant.
export type ViewSource = {
  key: string;
  participantIdentity: string;
  source: "camera" | "screenShare";
  local: boolean;
  available: boolean;
  recovering?: boolean;
};
export type ViewSelection = { mode: "auto" | "manual"; key: string | null };
export const sourceKey = (identity: string, source: ViewSource["source"]) =>
  JSON.stringify([identity, source]);
export function nextSelection(
  current: ViewSelection,
  sources: ViewSource[],
): ViewSelection {
  const valid = sources.filter(
    (s) => s.source === "camera" || s.available || s.recovering,
  );
  const previous = valid.find((s) => s.key === current.key);
  if (current.mode === "manual" && previous) return current;
  const shared =
    previous?.source === "screenShare"
      ? previous
      : valid.find((s) => s.source === "screenShare");
  const preferred =
    shared ||
    previous ||
    valid.find((s) => !s.local && s.available) ||
    valid.find((s) => s.local && s.available) ||
    valid[0];
  return { mode: "auto", key: preferred?.key || null };
}
