import { Link } from "react-router";
import type { Alert } from "../api/types";
import { useAcknowledgeAlert } from "../api/hooks";
import { formatAgo, formatNumber, humanizeKind } from "../lib/format";
import { useOperator } from "../lib/operator";
import "./AlertList.css";

const UNITS: Record<string, string> = {
  HIGH_LATENCY: "ms",
  PACKET_LOSS: "% loss",
  LOW_SNR: "dB SNR",
};

interface Props {
  alerts: Alert[];
  compact?: boolean;
}

export function AlertList({ alerts, compact = false }: Props) {
  const ack = useAcknowledgeAlert();
  const [operator] = useOperator();

  if (alerts.length === 0) {
    return <p className="empty">No open alerts. New ones appear here within 15 seconds of a terminal crossing a threshold.</p>;
  }

  return (
    <ol className={`alert-list ${compact ? "compact" : ""}`}>
      {alerts.map((a) => (
        <li key={a.id} className={`alert-item ${a.severity} ${a.acknowledgedAt ? "acked" : ""}`}>
          <div className="alert-main">
            <div className="alert-line">
              <span className={`severity ${a.severity}`}>{humanizeKind(a.kind)}</span>
              <Link to={`/terminals/${a.terminalId}`} className="alert-terminal">
                {a.terminalId}
              </Link>
              {a.observedValue !== null && UNITS[a.kind] && (
                <span className="alert-value">
                  {formatNumber(a.observedValue, 1)} {UNITS[a.kind]}
                </span>
              )}
            </div>
            <div className="alert-meta muted">
              <span>{a.customerName}</span>
              {!compact && <span>{a.region}</span>}
              <span>opened {formatAgo(a.openedAt)}</span>
              {a.resolvedAt && <span>resolved {formatAgo(a.resolvedAt)}</span>}
            </div>
            {!compact && <p className="alert-message">{a.message}</p>}
          </div>
          <div className="alert-actions">
            {a.resolvedAt ? null : a.acknowledgedAt ? (
              <span className="muted alert-acked">
                Acknowledged by {a.acknowledgedBy}
                {!compact && <>, {formatAgo(a.acknowledgedAt)}</>}
              </span>
            ) : (
              <button
                type="button"
                className="btn secondary small"
                disabled={ack.isPending && ack.variables?.id === a.id}
                onClick={() => ack.mutate({ id: a.id, by: operator })}
              >
                Acknowledge
              </button>
            )}
          </div>
        </li>
      ))}
    </ol>
  );
}
