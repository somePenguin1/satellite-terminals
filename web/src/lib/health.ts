import type { Health, TerminalStatus } from "../api/types";

export const HEALTH_ORDER: Health[] = ["healthy", "degraded", "down", "offline", "inactive"];

export const HEALTH_LABEL: Record<Health, string> = {
  healthy: "Healthy",
  degraded: "Degraded",
  down: "Link down",
  offline: "Offline",
  inactive: "Not in service",
};

/** Canvas needs literal colors; keep in sync with --health-* in tokens.css. */
export const HEALTH_COLOR: Record<Health, string> = {
  healthy: "#1F7A5C",
  degraded: "#B7791F",
  down: "#B42318",
  offline: "#6B7A86",
  inactive: "#A7B2BA",
};

export const STATUS_LABEL: Record<TerminalStatus, string> = {
  provisioned: "Awaiting activation",
  active: "Active",
  suspended: "Suspended",
  decommissioned: "Decommissioned",
};

/** Lifecycle transitions the API accepts, as offered in the UI. */
export const LIFECYCLE_ACTIONS: Record<TerminalStatus, { to: TerminalStatus; label: string; destructive?: boolean }[]> = {
  provisioned: [
    { to: "active", label: "Activate service" },
    { to: "decommissioned", label: "Cancel order", destructive: true },
  ],
  active: [
    { to: "suspended", label: "Suspend service" },
    { to: "decommissioned", label: "Decommission", destructive: true },
  ],
  suspended: [
    { to: "active", label: "Resume service" },
    { to: "decommissioned", label: "Decommission", destructive: true },
  ],
  decommissioned: [],
};
