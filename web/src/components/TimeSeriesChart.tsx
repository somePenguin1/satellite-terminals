import { useMemo, useState } from "react";
import { linePath, niceScale } from "../lib/chart";
import { formatClock } from "../lib/format";
import { useElementWidth } from "../lib/useElementWidth";
import "./TimeSeriesChart.css";

export interface Series {
  name: string;
  color: string;
  values: (number | null)[];
}

interface Props {
  title: string;
  unit: string;
  times: number[]; // epoch ms, one per value
  series: Series[];
  height?: number;
  digits?: number;
}

const PAD = { top: 12, right: 12, bottom: 24, left: 44 };

export function TimeSeriesChart({ title, unit, times, series, height = 180, digits = 1 }: Props) {
  const [ref, width] = useElementWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);

  const plotW = Math.max(10, width - PAD.left - PAD.right);
  const plotH = height - PAD.top - PAD.bottom;

  const { scale, xs, paths, xTicks, showSeconds } = useMemo(() => {
    const max = Math.max(0, ...series.flatMap((s) => s.values.filter((v): v is number => v !== null)));
    const scale = niceScale(max);
    const t0 = times[0] ?? 0;
    const t1 = times[times.length - 1] ?? t0 + 1;
    const span = Math.max(1, t1 - t0);
    const xs = times.map((t) => PAD.left + ((t - t0) / span) * plotW);
    const paths = series.map((s) =>
      linePath(
        xs,
        s.values.map((v) => (v === null ? null : PAD.top + plotH - (v / scale.max) * plotH)),
      ),
    );
    const tickCount = Math.max(2, Math.min(6, Math.floor(plotW / 90)));
    const xTicks = Array.from({ length: tickCount }, (_, i) => t0 + (span * i) / (tickCount - 1));
    // Live streams span seconds; label them with seconds so ticks don't all read the same minute.
    return { scale, xs, paths, xTicks, showSeconds: span < 10 * 60_000 };
  }, [series, times, plotW, plotH]);

  const latest = series.map((s) => [...s.values].reverse().find((v) => v !== null) ?? null);
  const summary = `${title}: ${series.map((s, i) => `${s.name} latest ${latest[i]?.toFixed(digits) ?? "no data"} ${unit}`).join(", ")}`;

  function onMove(e: React.MouseEvent<SVGSVGElement>) {
    if (xs.length === 0) return;
    const x = e.clientX - e.currentTarget.getBoundingClientRect().left;
    let best = 0;
    for (let i = 1; i < xs.length; i++) {
      if (Math.abs((xs[i] ?? 0) - x) < Math.abs((xs[best] ?? 0) - x)) best = i;
    }
    setHover(best);
  }

  const hoverX = hover === null ? null : xs[hover];
  const hoverTime = hover === null ? null : times[hover];

  return (
    <figure className="ts-chart" ref={ref}>
      <figcaption>
        <span className="ts-title">{title}</span>
        <span className="ts-legend">
          {series.map((s, i) => (
            <span key={s.name} className="ts-key">
              <span className="ts-swatch" style={{ background: s.color }} />
              {s.name}
              <strong>
                {(hover === null ? latest[i] : s.values[hover])?.toFixed(digits) ?? "–"} {unit}
              </strong>
            </span>
          ))}
        </span>
      </figcaption>
      {times.length === 0 ? (
        <div className="ts-empty" style={{ height }}>
          No samples in this window yet.
        </div>
      ) : (
        <svg
          width={width}
          height={height}
          role="img"
          aria-label={summary}
          onMouseMove={onMove}
          onMouseLeave={() => setHover(null)}
        >
          {scale.ticks.map((t) => {
            const y = PAD.top + plotH - (t / scale.max) * plotH;
            return (
              <g key={t}>
                <line x1={PAD.left} x2={PAD.left + plotW} y1={y} y2={y} className={t === 0 ? "ts-axis" : "ts-grid"} />
                <text x={PAD.left - 6} y={y} className="ts-tick" textAnchor="end" dominantBaseline="middle">
                  {t}
                </text>
              </g>
            );
          })}
          {xTicks.map((t, i) => (
            <text
              key={t}
              x={PAD.left + (plotW * i) / (xTicks.length - 1)}
              y={height - 6}
              className="ts-tick"
              textAnchor={i === 0 ? "start" : i === xTicks.length - 1 ? "end" : "middle"}
            >
              {formatClock(t, showSeconds)}
            </text>
          ))}
          {paths.map((d, i) => (
            <path key={series[i]?.name} d={d} fill="none" stroke={series[i]?.color} strokeWidth={1.75} />
          ))}
          {hoverX !== null && hoverX !== undefined && (
            <g>
              <line x1={hoverX} x2={hoverX} y1={PAD.top} y2={PAD.top + plotH} className="ts-cursor" />
              <text x={hoverX + 6} y={PAD.top + 10} className="ts-tick">
                {hoverTime ? formatClock(hoverTime, showSeconds) : ""}
              </text>
            </g>
          )}
        </svg>
      )}
    </figure>
  );
}
