// Wire types for the fleet API (Java, /api/v1 + /api/v2) and the live feed (Go, /ws/live).

export type Health = "healthy" | "degraded" | "down" | "offline" | "inactive";
export type TerminalStatus = "provisioned" | "active" | "suspended" | "decommissioned";
export type LinkState = "UP" | "DEGRADED" | "DOWN" | "UNSPECIFIED";
export type Segment = "residential" | "business" | "maritime" | "enterprise" | "government";

export interface LiveMetrics {
  lastSeenAt: string | null;
  linkState: LinkState | null;
  latencyMs: number | null;
  jitterMs: number | null;
  packetLossPct: number | null;
  downlinkMbps: number | null;
  uplinkMbps: number | null;
  snrDb: number | null;
  obstructionPct: number | null;
  firmwareVersion: string | null;
  transport: string | null;
}

export interface Plan {
  code: string;
  name: string;
  family: "residential" | "business" | "maritime";
  tier: number;
  downlinkMbps: number;
  uplinkMbps: number;
  monthlyPriceCents: number;
}

export interface TerminalSummary {
  id: string;
  status: TerminalStatus;
  health: Health;
  region: string;
  customer: { id: number; name: string };
  plan: { code: string; name: string };
  location: { lat: number; lon: number };
  metrics: LiveMetrics;
}

export interface CursorPage<T> {
  items: T[];
  nextCursor: string | null;
  total: number;
}

export interface TerminalDetail {
  id: string;
  serialNumber: string;
  status: TerminalStatus;
  health: Health;
  model: string;
  region: string;
  latitude: number;
  longitude: number;
  activatedAt: string | null;
  createdAt: string;
  customer: { id: number; name: string; segment: Segment; accountManager: string };
  plan: Plan;
  metrics: LiveMetrics;
}

export interface TerminalPatch {
  status?: TerminalStatus;
  planCode?: string;
}

export interface ProvisionRequest {
  customerId: number;
  planCode: string;
  model: string;
  latitude: number;
  longitude: number;
}

export type QosProfile = "standard" | "priority" | "business-critical";

/** Device configuration, v2 representation (grouped, interval in seconds). */
export interface DeviceConfig {
  terminalId: string;
  revision: number;
  reporting: { intervalSeconds: number; level: "basic" | "detailed" };
  radio: { band: "Ku" | "Ka"; txPowerDbm: number };
  network: { qosProfile: QosProfile };
  firmware: { channel: "stable" | "beta"; maintenanceWindow: { start: string; end: string } };
  shaping: { downlinkMbps: number; uplinkMbps: number };
  metadata: { updatedAt: string; updatedBy: string };
}

export type DeviceConfigUpdate = Pick<DeviceConfig, "reporting" | "radio" | "network" | "firmware">;

export interface ConfigHistoryEntry {
  version: number;
  changedBy: string;
  changedAt: string;
  config: Record<string, unknown>;
}

export interface TelemetryBucket {
  bucket: string;
  samples: number;
  latencyAvg: number | null;
  latencyMax: number | null;
  downlinkAvg: number | null;
  uplinkAvg: number | null;
  lossAvg: number | null;
  snrAvg: number | null;
}

export interface RegionHealth {
  region: string;
  total: number;
  healthy: number;
  degraded: number;
  down: number;
  offline: number;
  inactive: number;
  avgLatencyMs: number | null;
}

export interface FleetSummary {
  total: number;
  byStatus: Record<TerminalStatus, number>;
  byHealth: Record<Health, number>;
  regions: RegionHealth[];
  openAlerts: { critical: number; warning: number };
}

export interface FleetMinute {
  minute: string;
  samples: number;
  avgLatencyMs: number | null;
  avgDownlinkMbps: number | null;
  avgPacketLossPct: number | null;
  degradedPct: number | null;
}

/** Columnar to keep 5,000 points small on the wire. */
export interface FleetMap {
  ids: string[];
  lat: number[];
  lon: number[];
  health: Health[];
}

export type AlertSeverity = "critical" | "warning";

export interface Alert {
  id: number;
  terminalId: string;
  customerName: string;
  region: string;
  kind: string;
  severity: AlertSeverity;
  message: string;
  observedValue: number | null;
  openedAt: string;
  lastObservedAt: string;
  acknowledgedAt: string | null;
  acknowledgedBy: string | null;
  ackNote: string | null;
  resolvedAt: string | null;
}

export interface SalesSummary {
  mrrCents: number;
  activeSubscriptions: number;
  arpuCents: number;
  provisioningBacklog: number;
  suspended: number;
  byPlan: { code: string; name: string; family: string; terminals: number; mrrCents: number }[];
  byRegion: { region: string; terminals: number; mrrCents: number }[];
  bySegment: { segment: Segment; customers: number; terminals: number; mrrCents: number }[];
}

export interface PlanRef {
  code: string;
  name: string;
  downlinkMbps: number;
  monthlyPriceCents: number;
}

export interface UpsellCandidate {
  terminalId: string;
  customerId: number;
  customerName: string;
  accountManager: string;
  currentPlan: PlanRef;
  suggestedPlan: PlanRef;
  avgUtilizationPct: number;
  peakDownlinkMbps: number;
  monthlyDeltaCents: number;
}

export interface CustomerSummary {
  id: number;
  name: string;
  segment: Segment;
  region: string;
  accountManager: string;
  terminals: number;
  activeTerminals: number;
  mrrCents: number;
}

export interface CustomerDetail extends CustomerSummary {
  contactEmail: string | null;
  createdAt: string;
  terminalList: TerminalSummary[];
}

/** RFC 7807 problem detail returned by the fleet API on errors. */
export interface Problem {
  type?: string;
  title?: string;
  status?: number;
  detail?: string;
  errors?: Record<string, string>;
}

// ---- Live feed (Go ingest, /ws/live) ----

export interface IngestTick {
  type: "tick";
  ts: number;
  ingest: {
    acceptedPerSec: number;
    rejectedPerSec: number;
    totalAccepted: number;
    queueDepth: number;
    batchP99Ms: number;
    grpcStreams: number;
    wsDevices: number;
  };
}

export interface LiveReport {
  terminalId: string;
  timestampMs: number;
  seq: number;
  linkState: LinkState;
  latencyMs: number;
  jitterMs: number;
  packetLossPct: number;
  downlinkMbps: number;
  uplinkMbps: number;
  snrDb: number;
  obstructionPct: number;
}

export interface LiveTelemetryMessage {
  type: "telemetry";
  report: LiveReport;
}

export type LiveMessage = IngestTick | LiveTelemetryMessage;
