import { Link } from "react-router";
import { usePatchTerminal, useSalesSummary, useUpsellCandidates } from "../api/hooks";
import type { UpsellCandidate } from "../api/types";
import { ErrorNotice, LoadingRows } from "../components/QueryState";
import { capitalize, formatInt, formatMoney, formatNumber } from "../lib/format";
import "./SalesPage.css";

function UpgradeButton({ c }: { c: UpsellCandidate }) {
  const patch = usePatchTerminal(c.terminalId);
  if (patch.isSuccess) return <span className="muted">Moved to {c.suggestedPlan.name}</span>;
  return (
    <button
      type="button"
      className="btn secondary small"
      disabled={patch.isPending}
      title={patch.isError ? (patch.error as Error).message : undefined}
      onClick={() => patch.mutate({ planCode: c.suggestedPlan.code })}
    >
      {patch.isPending ? "Upgrading…" : `Upgrade to ${c.suggestedPlan.name}`}
    </button>
  );
}

function Bars({ rows }: { rows: { label: string; value: number; detail: string }[] }) {
  const max = Math.max(1, ...rows.map((r) => r.value));
  return (
    <ul className="bars">
      {rows.map((r) => (
        <li key={r.label}>
          <span className="bar-label">{r.label}</span>
          <span className="bar-track" aria-hidden>
            <span className="bar-fill" style={{ width: `${(100 * r.value) / max}%` }} />
          </span>
          <span className="bar-value">{r.detail}</span>
        </li>
      ))}
    </ul>
  );
}

export function SalesPage() {
  const summary = useSalesSummary();
  const upsell = useUpsellCandidates();
  const s = summary.data;
  const upsideCents = upsell.data?.reduce((sum, c) => sum + c.monthlyDeltaCents, 0) ?? 0;

  return (
    <>
      <header className="page-head">
        <div>
          <h1>Revenue</h1>
          <p className="sub">Recurring revenue from active subscriptions, and where customers are outgrowing their plans.</p>
        </div>
      </header>

      {summary.isError ? (
        <ErrorNotice error={summary.error} onRetry={() => void summary.refetch()} />
      ) : !s ? (
        <LoadingRows rows={4} />
      ) : (
        <>
          <section className="mrr-statement" aria-label="Revenue summary">
            <p className="mrr-figure">{formatMoney(s.mrrCents)}</p>
            <p className="mrr-text">
              monthly recurring revenue from {formatInt(s.activeSubscriptions)} active terminals, averaging{" "}
              {formatMoney(s.arpuCents)} each. {formatInt(s.provisioningBacklog)} orders are awaiting activation and{" "}
              {formatInt(s.suspended)} subscriptions are suspended.
            </p>
          </section>

          <div className="split-even section">
            <section>
              <div className="section-head">
                <h2>By plan</h2>
                <span className="meta">monthly revenue</span>
              </div>
              <Bars
                rows={s.byPlan.map((p) => ({
                  label: p.name,
                  value: p.mrrCents,
                  detail: `${formatMoney(p.mrrCents, true)}, ${formatInt(p.terminals)} terminals`,
                }))}
              />
            </section>
            <section>
              <div className="section-head">
                <h2>By segment</h2>
                <span className="meta">monthly revenue</span>
              </div>
              <Bars
                rows={s.bySegment.map((g) => ({
                  label: capitalize(g.segment),
                  value: g.mrrCents,
                  detail: `${formatMoney(g.mrrCents, true)}, ${formatInt(g.customers)} customers`,
                }))}
              />
            </section>
          </div>

          <section className="section">
            <div className="section-head">
              <h2>By region</h2>
            </div>
            <div className="table-wrap">
              <table className="data">
                <thead>
                  <tr>
                    <th>Region</th>
                    <th className="num">Active terminals</th>
                    <th className="num">Monthly revenue</th>
                    <th className="num">Per terminal</th>
                  </tr>
                </thead>
                <tbody>
                  {s.byRegion.map((r) => (
                    <tr key={r.region}>
                      <td className="strong">{r.region}</td>
                      <td className="num">{formatInt(r.terminals)}</td>
                      <td className="num">{formatMoney(r.mrrCents)}</td>
                      <td className="num">{r.terminals ? formatMoney(r.mrrCents / r.terminals) : "–"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        </>
      )}

      <section className="section">
        <div className="section-head">
          <h2>Upgrade opportunities</h2>
          <span className="meta">
            {upsell.data?.length
              ? `${formatMoney(upsideCents)} more per month if every suggestion is accepted`
              : "terminals averaging over 65% of their plan's downlink in the last 24 hours"}
          </span>
        </div>
        {upsell.isError ? (
          <ErrorNotice error={upsell.error} />
        ) : !upsell.data ? (
          <LoadingRows rows={6} />
        ) : upsell.data.length === 0 ? (
          <p className="empty">
            No terminal is consistently near its plan limit yet. Suggestions appear after the first full hour of usage data.
          </p>
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead>
                <tr>
                  <th>Terminal</th>
                  <th>Customer</th>
                  <th className="hide-sm">Account manager</th>
                  <th className="num">Plan used</th>
                  <th className="num hide-sm">Peak</th>
                  <th className="hide-sm">Current plan</th>
                  <th className="num">Added revenue</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {upsell.data.map((c) => (
                  <tr key={c.terminalId}>
                    <td className="strong">
                      <Link to={`/terminals/${c.terminalId}`}>{c.terminalId}</Link>
                    </td>
                    <td>
                      <Link to={`/customers/${c.customerId}`}>{c.customerName}</Link>
                    </td>
                    <td className="hide-sm">{c.accountManager}</td>
                    <td className="num">{formatNumber(c.avgUtilizationPct, 0)}%</td>
                    <td className="num hide-sm">
                      {formatNumber(c.peakDownlinkMbps, 0)} of {c.currentPlan.downlinkMbps} Mbps
                    </td>
                    <td className="hide-sm">{c.currentPlan.name}</td>
                    <td className="num">+{formatMoney(c.monthlyDeltaCents)}/mo</td>
                    <td className="num">
                      <UpgradeButton c={c} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </>
  );
}
