import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { useCustomers } from "../api/hooks";
import { ErrorNotice, LoadingRows } from "../components/QueryState";
import { capitalize, formatInt, formatMoney } from "../lib/format";

const SEGMENTS = ["residential", "business", "maritime", "enterprise", "government"];
const PAGE = 50;

export function CustomersPage() {
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  const [q, setQ] = useState("");
  const [segment, setSegment] = useState("");
  const [offset, setOffset] = useState(0);

  useEffect(() => {
    const t = setTimeout(() => {
      setQ(search);
      setOffset(0);
    }, 250);
    return () => clearTimeout(t);
  }, [search]);

  const customers = useCustomers({ q: q || undefined, segment: segment || undefined, offset });
  const total = customers.data?.total ?? 0;

  return (
    <>
      <header className="page-head">
        <div>
          <h1>Customers</h1>
          <p className="sub">Accounts, their terminals and what they're worth each month.</p>
        </div>
      </header>

      <form className="filters" role="search" onSubmit={(e) => e.preventDefault()}>
        <label className="field">
          Search
          <input type="search" placeholder="Customer name" value={search} onChange={(e) => setSearch(e.target.value)} />
        </label>
        <label className="field">
          Segment
          <select
            value={segment}
            onChange={(e) => {
              setSegment(e.target.value);
              setOffset(0);
            }}
          >
            <option value="">Any</option>
            {SEGMENTS.map((s) => (
              <option key={s} value={s}>
                {capitalize(s)}
              </option>
            ))}
          </select>
        </label>
      </form>

      {customers.isError ? (
        <ErrorNotice error={customers.error} onRetry={() => void customers.refetch()} />
      ) : !customers.data ? (
        <LoadingRows rows={12} />
      ) : customers.data.items.length === 0 ? (
        <p className="empty">No customers match that search.</p>
      ) : (
        <>
          <div className="table-wrap">
            <table className="data">
              <thead>
                <tr>
                  <th>Customer</th>
                  <th>Segment</th>
                  <th className="hide-sm">Region</th>
                  <th className="hide-sm">Account manager</th>
                  <th className="num">Terminals</th>
                  <th className="num">Monthly revenue</th>
                </tr>
              </thead>
              <tbody>
                {customers.data.items.map((c) => (
                  <tr key={c.id} className="clickable" onClick={() => navigate(`/customers/${c.id}`)}>
                    <td className="strong">{c.name}</td>
                    <td>{capitalize(c.segment)}</td>
                    <td className="hide-sm">{c.region}</td>
                    <td className="hide-sm">{c.accountManager}</td>
                    <td className="num">
                      {formatInt(c.activeTerminals)}
                      {c.terminals !== c.activeTerminals && <span className="muted"> of {formatInt(c.terminals)}</span>}
                    </td>
                    <td className="num">{formatMoney(c.mrrCents)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div style={{ marginTop: 16, display: "flex", gap: 12, alignItems: "center" }}>
            <button
              type="button"
              className="btn secondary"
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - PAGE))}
            >
              Previous
            </button>
            <button
              type="button"
              className="btn secondary"
              disabled={offset + PAGE >= total}
              onClick={() => setOffset(offset + PAGE)}
            >
              Next
            </button>
            <span className="muted">
              {formatInt(offset + 1)}–{formatInt(Math.min(offset + PAGE, total))} of {formatInt(total)}
            </span>
          </div>
        </>
      )}
    </>
  );
}
