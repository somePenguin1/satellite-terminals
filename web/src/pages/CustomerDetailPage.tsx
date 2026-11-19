import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { ApiError } from "../api/client";
import { useCustomer, usePlans, useProvisionTerminal } from "../api/hooks";
import type { CustomerDetail, Plan } from "../api/types";
import { ErrorNotice, LoadingRows } from "../components/QueryState";
import { capitalize, formatAgo, formatDate, formatInt, formatMoney } from "../lib/format";
import { HEALTH_LABEL, STATUS_LABEL } from "../lib/health";

const MODELS = ["Residential Kit G3", "Flat Panel Pro G2", "Enterprise Ka Dish", "Maritime Stabilized Ku"];

function familyFor(segment: CustomerDetail["segment"]): Plan["family"] {
  return segment === "residential" ? "residential" : segment === "maritime" ? "maritime" : "business";
}

function ProvisionForm({ customer }: { customer: CustomerDetail }) {
  const navigate = useNavigate();
  const plans = usePlans();
  const provision = useProvisionTerminal();
  const options = (plans.data ?? []).filter((p) => p.family === familyFor(customer.segment));
  const anchor = customer.terminalList[0]?.location;
  const [planCode, setPlanCode] = useState("");
  const [model, setModel] = useState(customer.segment === "maritime" ? MODELS[3]! : MODELS[1]!);
  const [lat, setLat] = useState(anchor ? anchor.lat.toFixed(4) : "");
  const [lon, setLon] = useState(anchor ? anchor.lon.toFixed(4) : "");

  const latN = Number(lat);
  const lonN = Number(lon);
  const coordsOk = lat !== "" && lon !== "" && Math.abs(latN) <= 90 && Math.abs(lonN) <= 180;
  const chosen = planCode || options[0]?.code || "";
  const fieldErrors = provision.error instanceof ApiError ? provision.error.problem.errors : undefined;

  return (
    <form
      className="filters"
      onSubmit={(e) => {
        e.preventDefault();
        if (!coordsOk || !chosen) return;
        provision.mutate(
          { customerId: customer.id, planCode: chosen, model, latitude: latN, longitude: lonN },
          { onSuccess: (t) => navigate(`/terminals/${t.id}`) },
        );
      }}
    >
      <label className="field">
        Plan
        <select value={chosen} onChange={(e) => setPlanCode(e.target.value)}>
          {options.map((p) => (
            <option key={p.code} value={p.code}>
              {p.name}, {formatMoney(p.monthlyPriceCents)}/mo
            </option>
          ))}
        </select>
      </label>
      <label className="field">
        Hardware
        <select value={model} onChange={(e) => setModel(e.target.value)}>
          {MODELS.map((m) => (
            <option key={m}>{m}</option>
          ))}
        </select>
      </label>
      <label className="field">
        Latitude
        <input inputMode="decimal" value={lat} onChange={(e) => setLat(e.target.value)} style={{ minWidth: "7rem" }} />
        {fieldErrors?.latitude && <span className="error">{fieldErrors.latitude}</span>}
      </label>
      <label className="field">
        Longitude
        <input inputMode="decimal" value={lon} onChange={(e) => setLon(e.target.value)} style={{ minWidth: "7rem" }} />
        {fieldErrors?.longitude && <span className="error">{fieldErrors.longitude}</span>}
      </label>
      <button type="submit" className="btn" disabled={!coordsOk || !chosen || provision.isPending}>
        {provision.isPending ? "Creating order…" : "Order terminal"}
      </button>
      {provision.isError && !fieldErrors && (
        <span className="field">
          <span className="error">{(provision.error as Error).message}</span>
        </span>
      )}
    </form>
  );
}

export function CustomerDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const customer = useCustomer(Number(id));

  if (customer.isError) return <ErrorNotice error={customer.error} onRetry={() => void customer.refetch()} />;
  const c = customer.data;
  if (!c) return <LoadingRows rows={8} />;

  return (
    <>
      <div className="crumb">
        <Link to="/customers">Customers</Link> / {c.name}
      </div>
      <header className="page-head">
        <div>
          <h1>{c.name}</h1>
          <p className="sub">
            {capitalize(c.segment)} customer in {c.region} since {formatDate(c.createdAt)}. Account manager{" "}
            {c.accountManager}
            {c.contactEmail && (
              <>
                , contact <a href={`mailto:${c.contactEmail}`}>{c.contactEmail}</a>
              </>
            )}
            .
          </p>
        </div>
      </header>

      <section className="mrr-statement" aria-label="Account value" style={{ borderBottom: 0, paddingBottom: 0 }}>
        <p className="mrr-text">
          <strong style={{ color: "var(--ink)" }}>{formatMoney(c.mrrCents)}</strong> per month from{" "}
          {formatInt(c.activeTerminals)} active terminals
          {c.terminals !== c.activeTerminals && ` (${formatInt(c.terminals - c.activeTerminals)} not in service)`}.
        </p>
      </section>

      <section className="section">
        <div className="section-head">
          <h2>Terminals</h2>
        </div>
        <div className="table-wrap">
          <table className="data">
            <thead>
              <tr>
                <th>Terminal</th>
                <th>Plan</th>
                <th>Service</th>
                <th>Health</th>
                <th className="hide-sm">Last report</th>
              </tr>
            </thead>
            <tbody>
              {c.terminalList.map((t) => (
                <tr key={t.id} className="clickable" onClick={() => navigate(`/terminals/${t.id}`)}>
                  <td className="strong">{t.id}</td>
                  <td>{t.plan.name}</td>
                  <td>{STATUS_LABEL[t.status]}</td>
                  <td>
                    <span className={`health ${t.health}`}>{HEALTH_LABEL[t.health]}</span>
                  </td>
                  <td className="hide-sm muted">{formatAgo(t.metrics.lastSeenAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="section">
        <div className="section-head">
          <h2>Order another terminal</h2>
          <span className="meta">creates the order; operations activates it after installation</span>
        </div>
        <ProvisionForm customer={c} />
      </section>
    </>
  );
}
