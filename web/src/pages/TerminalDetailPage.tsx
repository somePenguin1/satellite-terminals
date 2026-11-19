import { useState } from "react";
import { Link, useParams } from "react-router";
import {
  useAlerts,
  useConfigHistory,
  useDeviceConfig,
  usePatchTerminal,
  usePlans,
  useSaveDeviceConfig,
  useTerminal,
  useTerminalTelemetry,
} from "../api/hooks";
import { ApiError } from "../api/client";
import type { ConfigHistoryEntry, LiveMetrics, LiveReport, TerminalStatus } from "../api/types";
import { AlertList } from "../components/AlertList";
import { ConfigEditor } from "../components/ConfigEditor";
import { ErrorNotice, LoadingRows } from "../components/QueryState";
import { TimeSeriesChart } from "../components/TimeSeriesChart";
import { useLiveTerminal } from "../live/useLive";
import { formatAgo, formatDate, formatMoney, formatNumber } from "../lib/format";
import { HEALTH_LABEL, LIFECYCLE_ACTIONS, STATUS_LABEL } from "../lib/health";
import "./TerminalDetailPage.css";

const WINDOWS = [
  { minutes: 60, label: "1 hour" },
  { minutes: 360, label: "6 hours" },
  { minutes: 1440, label: "24 hours" },
];

function readingsFrom(live: LiveReport | undefined, rest: LiveMetrics | undefined) {
  if (live) {
    return {
      latencyMs: live.latencyMs,
      jitterMs: live.jitterMs,
      packetLossPct: live.packetLossPct,
      downlinkMbps: live.downlinkMbps,
      uplinkMbps: live.uplinkMbps,
      snrDb: live.snrDb,
      obstructionPct: live.obstructionPct,
      at: new Date(live.timestampMs).toISOString(),
    };
  }
  return { ...rest, at: rest?.lastSeenAt ?? null };
}

export function TerminalDetailPage() {
  const { id = "" } = useParams();
  const terminal = useTerminal(id);
  const [minutes, setMinutes] = useState(60);
  const telemetry = useTerminalTelemetry(id, minutes);
  const config = useDeviceConfig(id);
  const saveConfig = useSaveDeviceConfig(id);
  const history = useConfigHistory(id);
  const alerts = useAlerts({ state: "all", terminalId: id, limit: 10 });
  const patch = usePatchTerminal(id);
  const plans = usePlans();
  const live = useLiveTerminal(id);
  const [planCode, setPlanCode] = useState("");

  if (terminal.isError) {
    const notFound = terminal.error instanceof ApiError && terminal.error.status === 404;
    return notFound ? (
      <div className="empty">
        <h1>{id} not found</h1>
        <p style={{ marginTop: 8 }}>
          No terminal has this ID. <Link to="/terminals">Search the terminal list</Link>.
        </p>
      </div>
    ) : (
      <ErrorNotice error={terminal.error} onRetry={() => void terminal.refetch()} />
    );
  }
  const t = terminal.data;
  if (!t) return <LoadingRows rows={10} />;

  const latest = live[live.length - 1];
  const r = readingsFrom(latest, t.metrics);
  const buckets = telemetry.data ?? [];
  const times = buckets.map((b) => Date.parse(b.bucket));
  const familyPlans = (plans.data ?? []).filter((p) => p.family === t.plan.family);

  function changeStatus(to: TerminalStatus, label: string, destructive?: boolean) {
    if (destructive && !window.confirm(`${label} ${t!.id}? The terminal stops reporting and cannot be reactivated.`)) return;
    patch.mutate({ status: to });
  }

  return (
    <>
      <div className="crumb">
        <Link to="/terminals">Terminals</Link> / {t.id}
      </div>
      <header className="page-head terminal-head">
        <div>
          <h1>
            {t.id} <span className={`health ${t.health} head-health`}>{HEALTH_LABEL[t.health]}</span>
          </h1>
          <p className="sub">
            <Link to={`/customers/${t.customer.id}`}>{t.customer.name}</Link>, {t.region}. {t.model}, serial{" "}
            {t.serialNumber}.
          </p>
        </div>
        <div className="lifecycle">
          <span className="muted">{STATUS_LABEL[t.status]}</span>
          {LIFECYCLE_ACTIONS[t.status].map((a) => (
            <button
              key={a.to}
              type="button"
              className={`btn ${a.destructive ? "destructive" : "secondary"}`}
              disabled={patch.isPending}
              onClick={() => changeStatus(a.to, a.label, a.destructive)}
            >
              {a.label}
            </button>
          ))}
        </div>
      </header>
      {patch.isError && <ErrorNotice error={patch.error} />}

      <section aria-label="Current link readings">
        <dl className="readings">
          <div>
            <dt>Latency</dt>
            <dd>
              {formatNumber(r.latencyMs, 0)}
              <small>ms</small>
            </dd>
          </div>
          <div>
            <dt>Jitter</dt>
            <dd>
              {formatNumber(r.jitterMs, 1)}
              <small>ms</small>
            </dd>
          </div>
          <div>
            <dt>Packet loss</dt>
            <dd>
              {formatNumber(r.packetLossPct, 2)}
              <small>%</small>
            </dd>
          </div>
          <div>
            <dt>Downlink</dt>
            <dd>
              {formatNumber(r.downlinkMbps, 0)}
              <small>of {t.plan.downlinkMbps} Mbps</small>
            </dd>
          </div>
          <div>
            <dt>Uplink</dt>
            <dd>
              {formatNumber(r.uplinkMbps, 1)}
              <small>of {t.plan.uplinkMbps} Mbps</small>
            </dd>
          </div>
          <div>
            <dt>Signal to noise</dt>
            <dd>
              {formatNumber(r.snrDb, 1)}
              <small>dB</small>
            </dd>
          </div>
          <div>
            <dt>Sky obstruction</dt>
            <dd>
              {formatNumber(r.obstructionPct, 1)}
              <small>%</small>
            </dd>
          </div>
        </dl>
        <p className="readings-foot muted">
          {latest ? (
            <>
              <span className="live-dot open" aria-hidden /> Streaming live, last report {formatAgo(r.at)} (sequence{" "}
              {latest.seq})
            </>
          ) : (
            <>
              Last report {formatAgo(r.at)}
              {t.metrics.transport && ` over ${t.metrics.transport === "ws" ? "WebSocket" : "gRPC"}`}
              {t.metrics.firmwareVersion && `, firmware ${t.metrics.firmwareVersion}`}
            </>
          )}
        </p>
      </section>

      {live.length > 1 && (
        <section className="section">
          <div className="section-head">
            <h2>Live stream</h2>
            <span className="meta">every report since you opened this page</span>
          </div>
          <div className="split-even">
            <TimeSeriesChart
              title="Latency"
              unit="ms"
              digits={0}
              height={140}
              times={live.map((x) => x.timestampMs)}
              series={[{ name: "Round trip", color: "#172B3A", values: live.map((x) => x.latencyMs) }]}
            />
            <TimeSeriesChart
              title="Throughput"
              unit="Mbps"
              digits={0}
              height={140}
              times={live.map((x) => x.timestampMs)}
              series={[
                { name: "Down", color: "#1F7A5C", values: live.map((x) => x.downlinkMbps) },
                { name: "Up", color: "#A3195B", values: live.map((x) => x.uplinkMbps) },
              ]}
            />
          </div>
        </section>
      )}

      <section className="section">
        <div className="section-head">
          <h2>History</h2>
          <div className="window-picker" role="group" aria-label="Time window">
            {WINDOWS.map((w) => (
              <button
                key={w.minutes}
                type="button"
                aria-pressed={minutes === w.minutes}
                onClick={() => setMinutes(w.minutes)}
              >
                {w.label}
              </button>
            ))}
          </div>
        </div>
        {telemetry.isError ? (
          <ErrorNotice error={telemetry.error} />
        ) : (
          <div className="split-even">
            <TimeSeriesChart
              title="Latency"
              unit="ms"
              digits={0}
              times={times}
              series={[
                { name: "Average", color: "#172B3A", values: buckets.map((b) => b.latencyAvg) },
                { name: "Worst", color: "#B7791F", values: buckets.map((b) => b.latencyMax) },
              ]}
            />
            <TimeSeriesChart
              title="Throughput"
              unit="Mbps"
              digits={0}
              times={times}
              series={[
                { name: "Down", color: "#1F7A5C", values: buckets.map((b) => b.downlinkAvg) },
                { name: "Up", color: "#A3195B", values: buckets.map((b) => b.uplinkAvg) },
              ]}
            />
            <TimeSeriesChart
              title="Signal to noise"
              unit="dB"
              times={times}
              series={[{ name: "Average", color: "#172B3A", values: buckets.map((b) => b.snrAvg) }]}
            />
            <TimeSeriesChart
              title="Packet loss"
              unit="%"
              digits={2}
              times={times}
              series={[{ name: "Average", color: "#B42318", values: buckets.map((b) => b.lossAvg) }]}
            />
          </div>
        )}
      </section>

      <div className="split section">
        <section>
          <div className="section-head">
            <h2>Device configuration</h2>
            <span className="meta">applied by the terminal within 30 seconds</span>
          </div>
          {config.isError ? (
            <ErrorNotice error={config.error} />
          ) : config.data ? (
            <div className="sheet config-sheet">
              <ConfigEditor
                config={config.data.config}
                saving={saveConfig.isPending}
                error={saveConfig.error}
                readOnly={t.status === "decommissioned"}
                onSave={(update) => saveConfig.mutate({ update, etag: config.data.etag })}
                onReload={() => {
                  saveConfig.reset();
                  void config.refetch();
                }}
              />
            </div>
          ) : (
            <LoadingRows rows={4} />
          )}
          {saveConfig.isSuccess && !saveConfig.isPending && (
            <div className="notice success" role="status">
              <p>Configuration saved as revision {saveConfig.data.config.revision}.</p>
            </div>
          )}
        </section>

        <section>
          <div className="section-head">
            <h2>Service plan</h2>
            <span className="meta">{formatMoney(t.plan.monthlyPriceCents)} per month</span>
          </div>
          <p>
            {t.plan.name}: {t.plan.downlinkMbps}/{t.plan.uplinkMbps} Mbps. Account manager {t.customer.accountManager}.
            {t.activatedAt && ` In service since ${formatDate(t.activatedAt)}.`}
          </p>
          {t.status !== "decommissioned" && familyPlans.length > 1 && (
            <form
              className="filters"
              style={{ marginTop: 12 }}
              onSubmit={(e) => {
                e.preventDefault();
                if (planCode && planCode !== t.plan.code) patch.mutate({ planCode });
              }}
            >
              <label className="field">
                Move to plan
                <select value={planCode || t.plan.code} onChange={(e) => setPlanCode(e.target.value)}>
                  {familyPlans.map((p) => (
                    <option key={p.code} value={p.code}>
                      {p.name}, {formatMoney(p.monthlyPriceCents)}/mo
                    </option>
                  ))}
                </select>
              </label>
              <button type="submit" className="btn secondary" disabled={!planCode || planCode === t.plan.code || patch.isPending}>
                Change plan
              </button>
            </form>
          )}

          <div className="section-head section">
            <h2>Alerts</h2>
          </div>
          {alerts.data ? <AlertList alerts={alerts.data} compact /> : <LoadingRows rows={3} />}
        </section>
      </div>

      <section className="section">
        <div className="section-head">
          <h2>Configuration history</h2>
          <span className="meta">every revision, newest first</span>
        </div>
        {history.data ? <HistoryTable entries={history.data} /> : <LoadingRows rows={3} />}
      </section>
    </>
  );
}

function describeChanges(entry: ConfigHistoryEntry, previous: ConfigHistoryEntry | undefined): string {
  if (!previous) return "Initial configuration";
  const changed = Object.keys(entry.config).filter(
    (k) => JSON.stringify(entry.config[k]) !== JSON.stringify(previous.config[k]),
  );
  if (changed.length === 0) return "No field changes";
  return changed.map((k) => `${k.replace(/_/g, " ")} ${String(previous.config[k])} to ${String(entry.config[k])}`).join("; ");
}

function HistoryTable({ entries }: { entries: ConfigHistoryEntry[] }) {
  return (
    <div className="table-wrap">
      <table className="data">
        <thead>
          <tr>
            <th>Revision</th>
            <th>Changed by</th>
            <th>When</th>
            <th>What changed</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((e, i) => (
            <tr key={e.version}>
              <td className="strong">{e.version}</td>
              <td>{e.changedBy}</td>
              <td className="muted">{formatAgo(e.changedAt)}</td>
              <td>{describeChanges(e, entries[i + 1])}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
