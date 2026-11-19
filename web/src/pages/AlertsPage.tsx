import { useState } from "react";
import { useAlerts, type AlertFilters } from "../api/hooks";
import { AlertList } from "../components/AlertList";
import { ErrorNotice, LoadingRows } from "../components/QueryState";
import { useOperator } from "../lib/operator";

export function AlertsPage() {
  const [state, setState] = useState<AlertFilters["state"]>("open");
  const [severity, setSeverity] = useState("");
  const alerts = useAlerts({ state, severity: severity || undefined, limit: 200 });
  const [operator, setOperator] = useOperator();
  const [name, setName] = useState(operator);

  const unacked = alerts.data?.filter((a) => !a.acknowledgedAt && !a.resolvedAt).length ?? 0;

  return (
    <>
      <header className="page-head">
        <div>
          <h1>Alerts</h1>
          <p className="sub">
            Raised automatically when a terminal goes offline, loses its link, or crosses a latency, loss or signal
            threshold. They resolve on their own once readings recover.
          </p>
        </div>
      </header>

      <form className="filters" onSubmit={(e) => e.preventDefault()}>
        <label className="field">
          Show
          <select value={state} onChange={(e) => setState(e.target.value as AlertFilters["state"])}>
            <option value="open">Open</option>
            <option value="resolved">Resolved</option>
            <option value="all">All</option>
          </select>
        </label>
        <label className="field">
          Severity
          <select value={severity} onChange={(e) => setSeverity(e.target.value)}>
            <option value="">Any</option>
            <option value="critical">Critical</option>
            <option value="warning">Warning</option>
          </select>
        </label>
        <label className="field">
          Acknowledge as
          <input value={name} onChange={(e) => setName(e.target.value)} onBlur={() => setOperator(name)} />
        </label>
      </form>

      {state === "open" && alerts.data && (
        <p className="muted" style={{ marginBottom: 12 }}>
          {unacked === 0 ? "Every open alert has an owner." : `${unacked} open alerts have not been acknowledged.`}
        </p>
      )}

      {alerts.isError ? (
        <ErrorNotice error={alerts.error} onRetry={() => void alerts.refetch()} />
      ) : alerts.data ? (
        <AlertList alerts={alerts.data} />
      ) : (
        <LoadingRows rows={10} />
      )}
    </>
  );
}
