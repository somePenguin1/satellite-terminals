import { useEffect, useState } from "react";
import { ApiError } from "../api/client";
import type { DeviceConfig, DeviceConfigUpdate } from "../api/types";
import { formatAgo } from "../lib/format";
import "./ConfigEditor.css";

interface Props {
  config: DeviceConfig;
  saving: boolean;
  error: unknown;
  onSave: (update: DeviceConfigUpdate) => void;
  onReload: () => void;
  readOnly?: boolean;
}

type Draft = {
  intervalSeconds: string;
  level: DeviceConfig["reporting"]["level"];
  band: DeviceConfig["radio"]["band"];
  txPowerDbm: string;
  qosProfile: DeviceConfig["network"]["qosProfile"];
  channel: DeviceConfig["firmware"]["channel"];
  windowStart: string;
  windowEnd: string;
};

function toDraft(c: DeviceConfig): Draft {
  return {
    intervalSeconds: String(c.reporting.intervalSeconds),
    level: c.reporting.level,
    band: c.radio.band,
    txPowerDbm: String(c.radio.txPowerDbm),
    qosProfile: c.network.qosProfile,
    channel: c.firmware.channel,
    windowStart: c.firmware.maintenanceWindow.start,
    windowEnd: c.firmware.maintenanceWindow.end,
  };
}

/** Client-side checks mirror the API's bean validation so most mistakes never round-trip. */
export function validateDraft(d: Draft): Partial<Record<keyof Draft, string>> {
  const errors: Partial<Record<keyof Draft, string>> = {};
  const interval = Number(d.intervalSeconds);
  if (!Number.isInteger(interval) || interval < 1 || interval > 300) {
    errors.intervalSeconds = "Enter a whole number of seconds from 1 to 300.";
  }
  const tx = Number(d.txPowerDbm);
  if (d.txPowerDbm.trim() === "" || Number.isNaN(tx) || tx < 10 || tx > 33) {
    errors.txPowerDbm = "Transmit power must be between 10 and 33 dBm.";
  }
  if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(d.windowStart)) errors.windowStart = "Use 24-hour time, e.g. 02:00.";
  if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(d.windowEnd)) errors.windowEnd = "Use 24-hour time, e.g. 04:00.";
  return errors;
}

export function ConfigEditor({ config, saving, error, onSave, onReload, readOnly = false }: Props) {
  const [draft, setDraft] = useState<Draft>(() => toDraft(config));
  const [touched, setTouched] = useState(false);

  // When a newer revision arrives (after save or reload), reset the form to it.
  useEffect(() => {
    setDraft(toDraft(config));
    setTouched(false);
  }, [config]);

  const errors = validateDraft(draft);
  const dirty = JSON.stringify(draft) !== JSON.stringify(toDraft(config));
  const conflict = error instanceof ApiError && error.isConflict;

  function set<K extends keyof Draft>(key: K, value: Draft[K]) {
    setDraft((d) => ({ ...d, [key]: value }));
  }

  function submit(e: React.FormEvent) {
    e.preventDefault();
    setTouched(true);
    if (Object.keys(errors).length > 0 || !dirty) return;
    onSave({
      reporting: { intervalSeconds: Number(draft.intervalSeconds), level: draft.level },
      radio: { band: draft.band, txPowerDbm: Number(draft.txPowerDbm) },
      network: { qosProfile: draft.qosProfile },
      firmware: { channel: draft.channel, maintenanceWindow: { start: draft.windowStart, end: draft.windowEnd } },
    });
  }

  const show = (k: keyof Draft) => (touched || draft[k] !== toDraft(config)[k]) && errors[k];

  return (
    <form className="config-editor" onSubmit={submit} noValidate>
      <fieldset disabled={readOnly || saving}>
        <legend className="visually-hidden">Device configuration</legend>
        <div className="config-grid">
          <label className="field">
            Report every
            <span className="with-unit">
              <input
                inputMode="numeric"
                value={draft.intervalSeconds}
                onChange={(e) => set("intervalSeconds", e.target.value)}
                aria-invalid={!!show("intervalSeconds")}
              />
              <span>seconds</span>
            </span>
            {show("intervalSeconds") && <span className="error">{errors.intervalSeconds}</span>}
          </label>

          <label className="field">
            Telemetry detail
            <select value={draft.level} onChange={(e) => set("level", e.target.value as Draft["level"])}>
              <option value="basic">Basic</option>
              <option value="detailed">Detailed</option>
            </select>
          </label>

          <label className="field">
            Frequency band
            <select value={draft.band} onChange={(e) => set("band", e.target.value as Draft["band"])}>
              <option value="Ku">Ku band</option>
              <option value="Ka">Ka band</option>
            </select>
          </label>

          <label className="field">
            Transmit power
            <span className="with-unit">
              <input
                inputMode="decimal"
                value={draft.txPowerDbm}
                onChange={(e) => set("txPowerDbm", e.target.value)}
                aria-invalid={!!show("txPowerDbm")}
              />
              <span>dBm</span>
            </span>
            {show("txPowerDbm") && <span className="error">{errors.txPowerDbm}</span>}
          </label>

          <label className="field">
            Traffic priority
            <select value={draft.qosProfile} onChange={(e) => set("qosProfile", e.target.value as Draft["qosProfile"])}>
              <option value="standard">Standard</option>
              <option value="priority">Priority</option>
              <option value="business-critical">Business critical</option>
            </select>
          </label>

          <label className="field">
            Firmware channel
            <select value={draft.channel} onChange={(e) => set("channel", e.target.value as Draft["channel"])}>
              <option value="stable">Stable</option>
              <option value="beta">Beta</option>
            </select>
          </label>

          <div className="field">
            <span id="mw-label">Maintenance window (UTC)</span>
            <span className="with-unit" role="group" aria-labelledby="mw-label">
              <input
                aria-label="Window start"
                value={draft.windowStart}
                onChange={(e) => set("windowStart", e.target.value)}
                aria-invalid={!!show("windowStart")}
              />
              <span>to</span>
              <input
                aria-label="Window end"
                value={draft.windowEnd}
                onChange={(e) => set("windowEnd", e.target.value)}
                aria-invalid={!!show("windowEnd")}
              />
            </span>
            {(show("windowStart") || show("windowEnd")) && (
              <span className="error">{errors.windowStart ?? errors.windowEnd}</span>
            )}
          </div>

          <div className="field">
            Plan shaping
            <span className="readonly">
              {config.shaping.downlinkMbps} / {config.shaping.uplinkMbps} Mbps
            </span>
            <span className="hint">Set by the customer's plan</span>
          </div>
        </div>
      </fieldset>

      {conflict ? (
        <div className="notice warn" role="alert">
          <p>
            Someone else changed this configuration after you opened it. Reload to see revision{" "}
            {config.revision + 1} or later, then reapply your edits.
          </p>
          <button type="button" className="btn secondary small" style={{ marginTop: 8 }} onClick={onReload}>
            Reload configuration
          </button>
        </div>
      ) : error ? (
        <div className="notice error" role="alert">
          <p>{error instanceof Error ? error.message : "Saving failed."}</p>
        </div>
      ) : null}

      <div className="config-foot">
        <span className="muted">
          Revision {config.revision}, changed by {config.metadata.updatedBy} {formatAgo(config.metadata.updatedAt)}
        </span>
        {!readOnly && (
          <span className="config-buttons">
            {dirty && (
              <button type="button" className="btn secondary" onClick={() => setDraft(toDraft(config))} disabled={saving}>
                Discard changes
              </button>
            )}
            <button type="submit" className="btn" disabled={!dirty || saving}>
              {saving ? "Saving…" : "Save configuration"}
            </button>
          </span>
        )}
      </div>
    </form>
  );
}
