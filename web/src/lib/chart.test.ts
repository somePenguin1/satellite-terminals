import { describe, expect, it } from "vitest";
import { linePath, niceScale, niceStep, project } from "./chart";

describe("chart helpers", () => {
  it("picks human-friendly steps", () => {
    expect(niceStep(0.7)).toBe(1);
    expect(niceStep(13)).toBe(20);
    expect(niceStep(23)).toBe(25);
    expect(niceStep(380)).toBe(500);
    expect(niceStep(0)).toBe(1);
  });

  it("builds a zero-based scale that covers the max", () => {
    const s = niceScale(87);
    expect(s.ticks[0]).toBe(0);
    expect(s.max).toBeGreaterThanOrEqual(87);
    expect(s.ticks[s.ticks.length - 1]).toBe(s.max);
  });

  it("breaks lines at gaps", () => {
    expect(linePath([0, 10, 20, 30], [5, null, 7, 8])).toBe("M0.0,5.0M20.0,7.0L30.0,8.0");
  });

  it("projects the map corners", () => {
    expect(project(78, -180, 360, 136)).toEqual([0, 0]);
    expect(project(-58, 180, 360, 136)).toEqual([360, 136]);
  });
});
