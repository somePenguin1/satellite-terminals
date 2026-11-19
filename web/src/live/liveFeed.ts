import type { IngestTick, LiveMessage, LiveReport } from "../api/types";

export type ConnectionState = "connecting" | "open" | "closed";

const MAX_TICKS = 120; // two minutes of one-second ticks
const MAX_REPORTS = 240;

/** Appends to a bounded buffer, returning a new array so React sees the change. */
export function appendBounded<T>(buf: readonly T[], item: T, max: number): T[] {
  const next = buf.length >= max ? buf.slice(buf.length - max + 1) : buf.slice();
  next.push(item);
  return next;
}

type Listener = () => void;

/**
 * One shared WebSocket to the ingest service's /ws/live endpoint. Components subscribe to
 * fleet ticks and to individual terminals; the feed reference-counts terminal subscriptions
 * and replays them after a reconnect.
 */
export class LiveFeed {
  private ws: WebSocket | null = null;
  private listeners = new Set<Listener>();
  private terminalRefs = new Map<string, number>();
  private retryMs = 500;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private started = false;

  state: ConnectionState = "closed";
  ticks: IngestTick[] = [];
  reports = new Map<string, LiveReport[]>();

  constructor(
    private readonly url: string,
    private readonly makeSocket: (url: string) => WebSocket = (u) => new WebSocket(u),
  ) {}

  subscribe(listener: Listener): () => void {
    this.listeners.add(listener);
    this.start();
    return () => this.listeners.delete(listener);
  }

  watchTerminal(id: string): () => void {
    const n = this.terminalRefs.get(id) ?? 0;
    this.terminalRefs.set(id, n + 1);
    if (n === 0) this.send({ type: "subscribe", terminalIds: [id] });
    return () => {
      const left = (this.terminalRefs.get(id) ?? 1) - 1;
      if (left > 0) {
        this.terminalRefs.set(id, left);
        return;
      }
      this.terminalRefs.delete(id);
      this.reports.delete(id);
      this.send({ type: "unsubscribe", terminalIds: [id] });
    };
  }

  /** Applies one server message; exposed for tests. */
  handle(msg: LiveMessage): void {
    if (msg.type === "tick") {
      this.ticks = appendBounded(this.ticks, msg, MAX_TICKS);
    } else if (msg.type === "telemetry" && this.terminalRefs.has(msg.report.terminalId)) {
      const id = msg.report.terminalId;
      this.reports = new Map(this.reports).set(id, appendBounded(this.reports.get(id) ?? [], msg.report, MAX_REPORTS));
    }
    this.emit();
  }

  private start(): void {
    if (this.started) return;
    this.started = true;
    this.connect();
  }

  private connect(): void {
    this.setState("connecting");
    const ws = this.makeSocket(this.url);
    this.ws = ws;
    ws.onopen = () => {
      this.retryMs = 500;
      this.setState("open");
      const ids = [...this.terminalRefs.keys()];
      if (ids.length) this.send({ type: "subscribe", terminalIds: ids });
    };
    ws.onmessage = (ev: MessageEvent<string>) => {
      try {
        this.handle(JSON.parse(ev.data) as LiveMessage);
      } catch {
        // Ignore malformed frames rather than tearing down the feed.
      }
    };
    ws.onclose = () => {
      this.ws = null;
      this.setState("closed");
      // Exponential backoff with jitter, capped at 15 s.
      const delay = this.retryMs * (0.75 + Math.random() * 0.5);
      this.retryMs = Math.min(this.retryMs * 2, 15_000);
      this.retryTimer = setTimeout(() => this.connect(), delay);
    };
  }

  private send(msg: unknown): void {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(msg));
  }

  private setState(s: ConnectionState): void {
    this.state = s;
    this.emit();
  }

  private emit(): void {
    for (const l of this.listeners) l();
  }

  /** Test helper. */
  stop(): void {
    if (this.retryTimer) clearTimeout(this.retryTimer);
    if (this.ws) {
      this.ws.onclose = null;
      this.ws.close();
    }
  }
}

function defaultUrl(): string {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.host}/ws/live`;
}

let shared: LiveFeed | null = null;
export function liveFeed(): LiveFeed {
  shared ??= new LiveFeed(defaultUrl());
  return shared;
}
