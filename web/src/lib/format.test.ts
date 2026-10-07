import { describe, expect, it } from "vitest";

import { duration, percent, relative, rtt } from "./format";

const NOW = Date.parse("2026-10-07T12:00:00Z");
const ago = (ms: number) => new Date(NOW - ms).toISOString();

describe("relative", () => {
  it("says how long ago, at the precision a person needs", () => {
    expect(relative(ago(2_000), NOW)).toBe("just now");
    expect(relative(ago(30_000), NOW)).toBe("30 s ago");
    expect(relative(ago(5 * 60_000), NOW)).toBe("5 min ago");
    expect(relative(ago(3 * 3600_000), NOW)).toBe("3 h ago");
    expect(relative(ago(2 * 86400_000), NOW)).toBe("2 d ago");
  });

  it("does not fail on a clock that is ahead, or on nonsense", () => {
    expect(relative(new Date(NOW + 60_000).toISOString(), NOW)).toBe("just now");
    expect(relative("not a date", NOW)).toBe("unknown");
  });
});

describe("duration", () => {
  it("keeps the two largest units", () => {
    expect(duration(45_000)).toBe("45 s");
    expect(duration(3 * 60_000 + 20_000)).toBe("3 min");
    expect(duration(2 * 3600_000 + 5 * 60_000)).toBe("2 h 5 min");
    expect(duration(2 * 3600_000)).toBe("2 h");
    expect(duration(86400_000 + 3 * 3600_000)).toBe("1 d 3 h");
    expect(duration(3 * 86400_000)).toBe("3 d");
  });
});

describe("rtt", () => {
  it("scales the unit with the value", () => {
    expect(rtt(4.24)).toBe("4.2 ms");
    expect(rtt(12.6)).toBe("13 ms");
    expect(rtt(1250)).toBe("1.25 s");
  });
});

describe("percent", () => {
  it("never rounds up to a figure that is not true", () => {
    expect(percent(1)).toBe("100 %");
    expect(percent(0.9996)).toBe("99.9 %");
    expect(percent(0.75)).toBe("75 %");
    expect(percent(0.5)).toBe("50 %");
    expect(percent(0)).toBe("0 %");
    expect(percent(0.1234)).toBe("12.3 %");
  });
});
