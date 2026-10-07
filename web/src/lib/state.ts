export type RunState = "ok" | "failing" | "stale";

const STALE_FLOOR_MS = 5 * 60 * 1000;

/** A result older than three intervals, and five minutes, says nothing about now. */
export function runState(lastAt: string, ok: boolean, intervalSeconds: number, now: number = Date.now()): RunState {
  const age = now - Date.parse(lastAt);
  if (Number.isNaN(age) || age > Math.max(STALE_FLOOR_MS, 3 * intervalSeconds * 1000)) return "stale";
  return ok ? "ok" : "failing";
}
