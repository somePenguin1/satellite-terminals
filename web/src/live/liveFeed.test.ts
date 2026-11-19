import { afterEach, describe, expect, it, vi } from "vitest";
import type { IngestTick, LiveReport } from "../api/types";
import { appendBounded, LiveFeed } from "./liveFeed";

class FakeSocket {
  static OPEN = 1;
  readyState = 0;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.readyState = 3;
  }
  open() {
    this.readyState = 1;
    this.onopen?.();
  }
}

const tick = (n: number): IngestTick => ({
  type: "tick",
  ts: n,
  ingest: { acceptedPerSec: n, rejectedPerSec: 0, totalAccepted: n, queueDepth: 0, batchP99Ms: 1, grpcStreams: 1, wsDevices: 0 },
});
const report = (id: string, seq: number) => ({ terminalId: id, seq, timestampMs: seq }) as LiveReport;

describe("appendBounded", () => {
  it("keeps the newest items", () => {
    expect(appendBounded([1, 2, 3], 4, 3)).toEqual([2, 3, 4]);
    expect(appendBounded([], 1, 3)).toEqual([1]);
  });
});

describe("LiveFeed", () => {
  let sockets: FakeSocket[] = [];
  vi.stubGlobal("WebSocket", FakeSocket);
  const make = () => {
    sockets = [];
    return new LiveFeed("ws://test/ws/live", () => {
      const s = new FakeSocket();
      sockets.push(s);
      return s as unknown as WebSocket;
    });
  };

  afterEach(() => vi.useRealTimers());

  it("buffers ticks and notifies subscribers", () => {
    const feed = make();
    const listener = vi.fn();
    feed.subscribe(listener);
    for (let i = 0; i < 130; i++) feed.handle(tick(i));
    expect(feed.ticks).toHaveLength(120);
    expect(feed.ticks[0]?.ts).toBe(10);
    expect(listener).toHaveBeenCalled();
  });

  it("subscribes to a terminal once and ignores reports for others", () => {
    const feed = make();
    feed.subscribe(() => {});
    sockets[0]!.open();
    const stopA = feed.watchTerminal("SAT-00001");
    const stopB = feed.watchTerminal("SAT-00001");
    expect(sockets[0]!.sent.filter((m) => m.includes("subscribe"))).toHaveLength(1);

    feed.handle({ type: "telemetry", report: report("SAT-00001", 1) });
    feed.handle({ type: "telemetry", report: report("SAT-09999", 1) });
    expect(feed.reports.get("SAT-00001")).toHaveLength(1);
    expect(feed.reports.has("SAT-09999")).toBe(false);

    stopA();
    expect(sockets[0]!.sent.some((m) => m.includes("unsubscribe"))).toBe(false);
    stopB();
    expect(sockets[0]!.sent.some((m) => m.includes("unsubscribe"))).toBe(true);
  });

  it("reconnects with backoff and replays subscriptions", () => {
    vi.useFakeTimers();
    const feed = make();
    feed.subscribe(() => {});
    sockets[0]!.open();
    feed.watchTerminal("SAT-00042");
    sockets[0]!.onclose?.();
    expect(feed.state).toBe("closed");
    vi.advanceTimersByTime(1000);
    expect(sockets).toHaveLength(2);
    sockets[1]!.open();
    expect(feed.state).toBe("open");
    expect(sockets[1]!.sent[0]).toContain("SAT-00042");
    feed.stop();
  });
});
