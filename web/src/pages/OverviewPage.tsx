import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { useAlerts, useFleetMap, useFleetSeries, useFleetSummary } from "../api/hooks";
import type { Health } from "../api/types";
import { AlertList } from "../components/AlertList";
import { FleetMap } from "../components/FleetMap";
import { HealthBar } from "../components/HealthBar";
import { IngestTrace } from "../components/IngestTrace";
import { ErrorNotice, LoadingRows } from "../components/QueryState";
import { TimeSeriesChart } from "../components/TimeSeriesChart";
import { useIngestTicks } from "../live/useLive";
import { formatInt, formatNumber } from "../lib/format";

export function OverviewPage() {
  const navigate = useNavigate();
  const summary = useFleetSummary();
  const map = useFleetMap();
  const series = useFleetSeries(60);
  const alerts = useAlerts({ state: "open", limit: 12 });
  const { ticks, state } = useIngestTicks();
  const [focus, setFocus] = useState<Health | null>(null);

  const s = summary.data;
  const inService = s ? s.byStatus.active + s.byStatus.suspended : 0;
  const times = series.data?.map((m) => Date.parse(m.minute)) ?? [];

  return (
    <>
      <header className="page-head">
        <div>
          <h1>Fleet overview</h1>
          <p className="sub">
            {s
              ? `${formatInt(inService)} terminals in service across ${s.regions.length} regions, ${formatInt(s.byStatus.provisioned)} awaiting activation.`
              : "Loading fleet status…"}
          </p>
        </div>
      </header>

      <section className="sheet" aria-label="Fleet map and live ingest">
        <FleetMap data={map.data} highlight={focus} onSelect={(id) => navigate(`/terminals/${id}`)} />
        <IngestTrace ticks={ticks} state={state} />
      </section>

      <section className="section" aria-label="Fleet health">
        {summary.isError ? (
          <ErrorNotice error={summary.error} onRetry={() => void summary.refetch()} />
        ) : s ? (
          <HealthBar counts={s.byHealth} selected={focus} onSelect={setFocus} />
        ) : (
          <LoadingRows rows={2} />
        )}
      </section>

      <div className="split section">
        <div>
          <section>
            <div className="section-head">
              <h2>Regions</h2>
              <Link to="/terminals" className="meta">
                Browse terminals
              </Link>
            </div>
            {s ? (
              <div className="table-wrap">
                <table className="data">
                  <thead>
                    <tr>
                      <th>Region</th>
                      <th className="num">Terminals</th>
                      <th className="num">Healthy</th>
                      <th className="num">Degraded</th>
                      <th className="num">Down</th>
                      <th className="num hide-sm">Offline</th>
                      <th className="num">Latency</th>
                    </tr>
                  </thead>
                  <tbody>
                    {s.regions.map((r) => {
                      const live = r.total - r.inactive;
                      return (
                        <tr
                          key={r.region}
                          className="clickable"
                          onClick={() => navigate(`/terminals?region=${encodeURIComponent(r.region)}`)}
                        >
                          <td className="strong">{r.region}</td>
                          <td className="num">{formatInt(r.total)}</td>
                          <td className="num">{live ? `${((100 * r.healthy) / live).toFixed(1)}%` : "–"}</td>
                          <td className="num" style={{ color: r.degraded ? "var(--degraded)" : undefined }}>
                            {formatInt(r.degraded)}
                          </td>
                          <td className="num" style={{ color: r.down ? "var(--down)" : undefined }}>
                            {formatInt(r.down)}
                          </td>
                          <td className="num hide-sm">{formatInt(r.offline)}</td>
                          <td className="num">{r.avgLatencyMs === null ? "–" : `${formatNumber(r.avgLatencyMs, 0)} ms`}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            ) : (
              <LoadingRows rows={7} />
            )}
          </section>

          <section className="section">
            <div className="section-head">
              <h2>Link quality, last hour</h2>
              <span className="meta">fleet-wide averages per minute</span>
            </div>
            {series.isError ? (
              <ErrorNotice error={series.error} />
            ) : (
              <div className="split-even">
                <TimeSeriesChart
                  title="Latency"
                  unit="ms"
                  digits={0}
                  times={times}
                  series={[{ name: "Average", color: "#172B3A", values: series.data?.map((m) => m.avgLatencyMs) ?? [] }]}
                />
                <TimeSeriesChart
                  title="Packet loss"
                  unit="%"
                  digits={2}
                  times={times}
                  series={[{ name: "Average", color: "#B7791F", values: series.data?.map((m) => m.avgPacketLossPct) ?? [] }]}
                />
              </div>
            )}
          </section>
        </div>

        <section>
          <div className="section-head">
            <h2>Needs attention</h2>
            <Link to="/alerts" className="meta">
              {s ? `${s.openAlerts.critical} critical, ${s.openAlerts.warning} warning` : "All alerts"}
            </Link>
          </div>
          {alerts.isError ? (
            <ErrorNotice error={alerts.error} />
          ) : alerts.data ? (
            <AlertList alerts={alerts.data} compact />
          ) : (
            <LoadingRows rows={6} />
          )}
        </section>
      </div>
    </>
  );
}
