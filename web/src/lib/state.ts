export type RunState = "ok" | "failing" | "stale";

const STALE_FLOOR_MS = 5 * 60 * 1000;

/**
 * What the latest run of a check on an edge says now. A result that is old
 * says nothing about the present, however good it was: past three intervals
 * (and at least five minutes) the edge is no longer measuring.
 */
export function runState(lastAt: string, ok: boolean, intervalSeconds: number, now: number = Date.now()): RunState {
  const age = now - Date.parse(lastAt);
  if (Number.isNaN(age) || age > Math.max(STALE_FLOOR_MS, 3 * intervalSeconds * 1000)) return "stale";
  return ok ? "ok" : "failing";
}
