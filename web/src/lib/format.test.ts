import { describe, expect, it } from "vitest";
import { formatAgo, formatClock, formatInt, formatMoney, formatNumber, humanizeKind } from "./format";

describe("format", () => {
  it("formats integers and missing values", () => {
    expect(formatInt(4751)).toBe("4,751");
    expect(formatInt(null)).toBe("–");
    expect(formatNumber(undefined)).toBe("–");
    expect(formatNumber(41.256, 1)).toBe("41.3");
  });

  it("formats money from cents", () => {
    expect(formatMoney(1650000)).toBe("$16,500");
    expect(formatMoney(123456789, true)).toBe("$1.2M");
  });

  it("describes elapsed time in plain words", () => {
    const now = Date.parse("2026-11-19T12:00:00Z");
    expect(formatAgo("2026-11-19T11:59:58Z", now)).toBe("just now");
    expect(formatAgo("2026-11-19T11:59:18Z", now)).toBe("42 s ago");
    expect(formatAgo("2026-11-19T11:57:00Z", now)).toBe("3 min ago");
    expect(formatAgo("2026-11-17T13:00:00Z", now)).toBe("47 h ago");
    expect(formatAgo("2026-11-17T12:00:00Z", now)).toBe("2 d ago");
    expect(formatAgo("2026-11-10T12:00:00Z", now)).toBe("9 d ago");
    expect(formatAgo(null, now)).toBe("never");
  });

  it("humanizes alert kinds", () => {
    expect(humanizeKind("HIGH_LATENCY")).toBe("High latency");
    expect(humanizeKind("OFFLINE")).toBe("Offline");
  });
});

describe("formatClock", () => {
  it("adds seconds only when asked", () => {
    const t = Date.UTC(2026, 10, 19, 14, 15, 42);
    expect(formatClock(t)).toMatch(/^\d{2}:\d{2}$/);
    expect(formatClock(t, true)).toMatch(/^\d{2}:\d{2}:42$/);
  });
});
