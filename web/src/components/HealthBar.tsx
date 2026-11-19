import type { Health } from "../api/types";
import { formatInt } from "../lib/format";
import { HEALTH_LABEL, HEALTH_ORDER } from "../lib/health";
import "./HealthBar.css";

interface Props {
  counts: Record<Health, number>;
  selected?: Health | null;
  onSelect?: (h: Health | null) => void;
}

/** Fleet health as one proportional bar; each segment doubles as a map filter. */
export function HealthBar({ counts, selected = null, onSelect }: Props) {
  const total = HEALTH_ORDER.reduce((s, h) => s + (counts[h] ?? 0), 0);
  return (
    <div className="health-bar">
      <div className="hb-track" aria-hidden>
        {HEALTH_ORDER.map((h) =>
          counts[h] ? (
            <span
              key={h}
              className={`hb-seg ${h} ${selected && selected !== h ? "dim" : ""}`}
              style={{ flexGrow: counts[h] }}
            />
          ) : null,
        )}
      </div>
      <ul className="hb-legend">
        {HEALTH_ORDER.map((h) => (
          <li key={h}>
            <button
              type="button"
              className={`hb-item ${selected === h ? "selected" : ""}`}
              aria-pressed={selected === h}
              onClick={() => onSelect?.(selected === h ? null : h)}
              disabled={!onSelect}
            >
              <span className="hb-count">{formatInt(counts[h] ?? 0)}</span>
              <span className={`health ${h}`}>{HEALTH_LABEL[h]}</span>
              <span className="hb-share muted">{total ? `${((100 * (counts[h] ?? 0)) / total).toFixed(1)}%` : ""}</span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
