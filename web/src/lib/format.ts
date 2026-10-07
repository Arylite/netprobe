const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** "5 min ago": how long before now, at the precision a person needs. */
export function relative(iso: string, now: number = Date.now()): string {
  const ms = now - Date.parse(iso);
  if (Number.isNaN(ms)) return "unknown";
  if (ms < 0) return "just now";
  return ms < 5 * SECOND ? "just now" : `${duration(ms)} ago`;
}

/** "2 h 5 min": a span of time with its two largest units. */
export function duration(ms: number): string {
  if (ms < MINUTE) return `${Math.max(Math.round(ms / SECOND), 0)} s`;
  if (ms < HOUR) return `${Math.floor(ms / MINUTE)} min`;
  if (ms < DAY) {
    const minutes = Math.floor((ms % HOUR) / MINUTE);
    return minutes ? `${Math.floor(ms / HOUR)} h ${minutes} min` : `${Math.floor(ms / HOUR)} h`;
  }
  const hours = Math.floor((ms % DAY) / HOUR);
  return hours ? `${Math.floor(ms / DAY)} d ${hours} h` : `${Math.floor(ms / DAY)} d`;
}

/** "12 ms", "4.2 ms" or "1.25 s". */
export function rtt(millis: number): string {
  if (millis >= 1000) return `${(millis / 1000).toFixed(2)} s`;
  return millis < 10 ? `${millis.toFixed(1)} ms` : `${Math.round(millis)} ms`;
}

/** "99.9 %": rounds down, so 99.96 reads 99.9 and only a perfect score reads 100. */
export function percent(ratio: number): string {
  const tenths = Math.floor(ratio * 1000 + 1e-9) / 10;
  return `${Number.isInteger(tenths) ? tenths : tenths.toFixed(1)} %`;
}

/** The full date and time, for a tooltip. */
export function fullDate(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}
