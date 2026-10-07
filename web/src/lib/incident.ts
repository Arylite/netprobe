import type { Incident } from "../api/client";
import { duration } from "./format";

/** What an incident is about, in a sentence. */
export function subject(i: Incident): string {
  return i.kind === "check" ? `${i.check_id ?? "?"} on ${i.edge}` : `${i.edge} stopped reporting`;
}

/** How long it lasted, or has lasted so far. */
export function lasted(i: Incident, now: number = Date.now()): string {
  const end = i.resolved_at ? Date.parse(i.resolved_at) : now;
  return duration(Math.max(end - Date.parse(i.started_at), 0));
}
