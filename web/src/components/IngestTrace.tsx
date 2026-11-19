import type { IngestTick } from "../api/types";
import type { ConnectionState } from "../live/liveFeed";
import { niceScale } from "../lib/chart";
import { formatInt, formatNumber } from "../lib/format";
import { useElementWidth } from "../lib/useElementWidth";
import "./IngestTrace.css";

interface Props {
  ticks: IngestTick[];
  state: ConnectionState;
}

const WINDOW = 120; // seconds shown
const H = 56;

/** Oscilloscope-style strip of accepted reports per second over the last two minutes. */
export function IngestTrace({ ticks, state }: Props) {
  const [ref, width] = useElementWidth<HTMLDivElement>(900);
  const last = ticks[ticks.length - 1];
  const { max } = niceScale(Math.max(10, ...ticks.map((t) => t.ingest.acceptedPerSec)), 2);

  const step = width / (WINDOW - 1);
  const offset = WINDOW - ticks.length;
  const pts = ticks.map((t, i) => [(offset + i) * step, H - 4 - (t.ingest.acceptedPerSec / max) * (H - 10)] as const);
  const line = pts.map(([x, y], i) => `${i ? "L" : "M"}${x.toFixed(1)},${y.toFixed(1)}`).join("");
  const area = pts.length ? `${line}L${pts[pts.length - 1]![0].toFixed(1)},${H}L${pts[0]![0].toFixed(1)},${H}Z` : "";

  return (
    <div className="ingest-trace">
      <div className="trace-readout" aria-live="polite">
        <span className={`live-dot ${state}`} aria-hidden />
        {state === "open" && last ? (
          <>
            <span>
              <strong>{formatInt(last.ingest.acceptedPerSec)}</strong> reports/s ingested
            </span>
            <span className="muted hide-sm">
              {formatInt(last.ingest.grpcStreams)} gRPC streams, {formatInt(last.ingest.wsDevices)} WebSocket agents
            </span>
            <span className="muted hide-sm">batch p99 {formatNumber(last.ingest.batchP99Ms, 1)} ms</span>
            {last.ingest.rejectedPerSec > 0 && (
              <span className="severity warning">{formatInt(last.ingest.rejectedPerSec)} rejected/s</span>
            )}
          </>
        ) : (
          <span className="muted">{state === "connecting" ? "Connecting to the live feed…" : "Live feed disconnected, retrying"}</span>
        )}
        <span className="trace-scale muted">peak scale {formatInt(max)}/s, last 2 min</span>
      </div>
      <div ref={ref}>
        <svg width={width} height={H} aria-hidden>
          <line x1={0} x2={width} y1={H - 0.5} y2={H - 0.5} className="trace-base" />
          {area && <path d={area} className="trace-area" />}
          {line && <path d={line} className="trace-line" />}
        </svg>
      </div>
    </div>
  );
}
