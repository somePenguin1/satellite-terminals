import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { useTerminals, type TerminalFilters } from "../api/hooks";
import { ErrorNotice, LoadingRows } from "../components/QueryState";
import { formatAgo, formatInt, formatNumber } from "../lib/format";
import { HEALTH_LABEL, HEALTH_ORDER, STATUS_LABEL } from "../lib/health";

const REGIONS = ["Africa", "Asia Pacific", "Europe", "Maritime", "North America", "Oceania", "South America"];

export function TerminalsPage() {
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const filters: TerminalFilters = {
    q: params.get("q") ?? undefined,
    status: params.get("status") ?? undefined,
    health: params.get("health") ?? undefined,
    region: params.get("region") ?? undefined,
  };
  const [search, setSearch] = useState(filters.q ?? "");

  // Debounce typing so we query once the operator pauses.
  useEffect(() => {
    const t = setTimeout(() => setFilter("q", search), 250);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search]);

  function setFilter(key: keyof TerminalFilters, value: string) {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value) next.set(key, value);
        else next.delete(key);
        return next;
      },
      { replace: true },
    );
  }

  const query = useTerminals(filters);
  const items = query.data?.pages.flatMap((p) => p.items) ?? [];
  const total = query.data?.pages[0]?.total;

  return (
    <>
      <header className="page-head">
        <div>
          <h1>Terminals</h1>
          <p className="sub">
            {total === undefined ? "Loading…" : `${formatInt(total)} terminals match these filters.`}
          </p>
        </div>
      </header>

      <form className="filters" role="search" onSubmit={(e) => e.preventDefault()}>
        <label className="field">
          Search
          <input
            type="search"
            placeholder="Terminal ID, serial or customer"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </label>
        <label className="field">
          Health
          <select value={filters.health ?? ""} onChange={(e) => setFilter("health", e.target.value)}>
            <option value="">Any</option>
            {HEALTH_ORDER.map((h) => (
              <option key={h} value={h}>
                {HEALTH_LABEL[h]}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          Service status
          <select value={filters.status ?? ""} onChange={(e) => setFilter("status", e.target.value)}>
            <option value="">Any</option>
            {Object.entries(STATUS_LABEL).map(([v, label]) => (
              <option key={v} value={v}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          Region
          <select value={filters.region ?? ""} onChange={(e) => setFilter("region", e.target.value)}>
            <option value="">Any</option>
            {REGIONS.map((r) => (
              <option key={r}>{r}</option>
            ))}
          </select>
        </label>
        {(filters.q || filters.health || filters.status || filters.region) && (
          <button
            type="button"
            className="btn secondary"
            onClick={() => {
              setSearch("");
              setParams({}, { replace: true });
            }}
          >
            Clear filters
          </button>
        )}
      </form>

      {query.isError ? (
        <ErrorNotice error={query.error} onRetry={() => void query.refetch()} />
      ) : query.isPending ? (
        <LoadingRows rows={12} />
      ) : items.length === 0 ? (
        <p className="empty">No terminals match. Try clearing a filter or searching by customer name.</p>
      ) : (
        <>
          <div className="table-wrap">
            <table className="data">
              <thead>
                <tr>
                  <th>Terminal</th>
                  <th>Customer</th>
                  <th className="hide-sm">Plan</th>
                  <th className="hide-sm">Region</th>
                  <th>Health</th>
                  <th className="num">Latency</th>
                  <th className="num hide-sm">Downlink</th>
                  <th className="num hide-sm">SNR</th>
                  <th className="hide-sm">Last report</th>
                </tr>
              </thead>
              <tbody>
                {items.map((t) => (
                  <tr key={t.id} className="clickable" onClick={() => navigate(`/terminals/${t.id}`)}>
                    <td className="strong">
                      <a href={`/terminals/${t.id}`} onClick={(e) => e.preventDefault()}>
                        {t.id}
                      </a>
                      {t.status !== "active" && <div className="muted">{STATUS_LABEL[t.status]}</div>}
                    </td>
                    <td>{t.customer.name}</td>
                    <td className="hide-sm">{t.plan.name}</td>
                    <td className="hide-sm">{t.region}</td>
                    <td>
                      <span className={`health ${t.health}`}>{HEALTH_LABEL[t.health]}</span>
                    </td>
                    <td className="num">{t.metrics.latencyMs === null ? "–" : `${formatNumber(t.metrics.latencyMs, 0)} ms`}</td>
                    <td className="num hide-sm">
                      {t.metrics.downlinkMbps === null ? "–" : `${formatNumber(t.metrics.downlinkMbps, 0)} Mbps`}
                    </td>
                    <td className="num hide-sm">{t.metrics.snrDb === null ? "–" : `${formatNumber(t.metrics.snrDb, 1)} dB`}</td>
                    <td className="hide-sm muted">{formatAgo(t.metrics.lastSeenAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div style={{ marginTop: 16, display: "flex", gap: 16, alignItems: "center" }}>
            {query.hasNextPage && (
              <button
                type="button"
                className="btn secondary"
                onClick={() => void query.fetchNextPage()}
                disabled={query.isFetchingNextPage}
              >
                {query.isFetchingNextPage ? "Loading…" : "Show 50 more"}
              </button>
            )}
            <span className="muted">
              Showing {formatInt(items.length)} of {formatInt(total ?? items.length)}
            </span>
          </div>
        </>
      )}
    </>
  );
}
