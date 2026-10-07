import { describe, expect, it } from "vitest";

import { runState } from "./state";

const NOW = Date.parse("2026-10-07T12:00:00Z");
const ago = (s: number) => new Date(NOW - s * 1000).toISOString();

describe("runState", () => {
  it("follows the latest run while it is recent", () => {
    expect(runState(ago(20), true, 30, NOW)).toBe("ok");
    expect(runState(ago(20), false, 30, NOW)).toBe("failing");
  });

  it("calls a result stale once it is older than three intervals and five minutes", () => {
    expect(runState(ago(299), true, 30, NOW)).toBe("ok");
    expect(runState(ago(301), true, 30, NOW)).toBe("stale");
    // A slow check is given its three intervals.
    expect(runState(ago(1000), true, 600, NOW)).toBe("ok");
    expect(runState(ago(1900), false, 600, NOW)).toBe("stale");
  });

  it("does not trust a date it cannot read", () => {
    expect(runState("not a date", true, 30, NOW)).toBe("stale");
  });
});
