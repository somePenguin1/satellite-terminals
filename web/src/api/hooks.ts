import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getJson, qs, request } from "./client";
import type {
  Alert,
  ConfigHistoryEntry,
  CursorPage,
  CustomerDetail,
  CustomerSummary,
  DeviceConfig,
  DeviceConfigUpdate,
  FleetMap,
  FleetMinute,
  FleetSummary,
  Plan,
  ProvisionRequest,
  SalesSummary,
  TelemetryBucket,
  TerminalDetail,
  TerminalPatch,
  TerminalSummary,
  UpsellCandidate,
} from "./types";

// Operational data refreshes on a short interval; commercial data changes slowly.
const LIVE = 10_000;
const SLOW = 60_000;

export const keys = {
  fleetSummary: ["fleet", "summary"] as const,
  fleetMap: ["fleet", "map"] as const,
  fleetSeries: (minutes: number) => ["fleet", "timeseries", minutes] as const,
  terminals: (filters: TerminalFilters) => ["terminals", filters] as const,
  terminal: (id: string) => ["terminal", id] as const,
  telemetry: (id: string, minutes: number) => ["terminal", id, "telemetry", minutes] as const,
  config: (id: string) => ["terminal", id, "config"] as const,
  configHistory: (id: string) => ["terminal", id, "config-history"] as const,
  alerts: (f: AlertFilters) => ["alerts", f] as const,
  sales: ["sales", "summary"] as const,
  upsell: ["sales", "upsell"] as const,
  customers: (f: CustomerFilters) => ["customers", f] as const,
  customer: (id: number) => ["customer", id] as const,
  plans: ["plans"] as const,
};

export function useFleetSummary() {
  return useQuery({
    queryKey: keys.fleetSummary,
    queryFn: ({ signal }) => getJson<FleetSummary>("/api/v1/fleet/summary", signal),
    refetchInterval: LIVE,
  });
}

export function useFleetMap() {
  return useQuery({
    queryKey: keys.fleetMap,
    queryFn: ({ signal }) => getJson<FleetMap>("/api/v1/fleet/map", signal),
    refetchInterval: 15_000,
  });
}

export function useFleetSeries(minutes = 60) {
  return useQuery({
    queryKey: keys.fleetSeries(minutes),
    queryFn: ({ signal }) => getJson<FleetMinute[]>(`/api/v1/fleet/timeseries${qs({ minutes })}`, signal),
    refetchInterval: 30_000,
  });
}

export interface TerminalFilters {
  q?: string;
  status?: string;
  health?: string;
  region?: string;
}

/** Keyset-paginated terminal list from the v2 API. */
export function useTerminals(filters: TerminalFilters) {
  return useInfiniteQuery({
    queryKey: keys.terminals(filters),
    initialPageParam: "",
    queryFn: ({ pageParam, signal }) =>
      getJson<CursorPage<TerminalSummary>>(`/api/v2/terminals${qs({ ...filters, cursor: pageParam, limit: 50 })}`, signal),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    placeholderData: keepPreviousData,
    refetchInterval: 30_000,
  });
}

export function useTerminal(id: string) {
  return useQuery({
    queryKey: keys.terminal(id),
    queryFn: ({ signal }) => getJson<TerminalDetail>(`/api/v1/terminals/${encodeURIComponent(id)}`, signal),
    refetchInterval: LIVE,
  });
}

export function useTerminalTelemetry(id: string, minutes: number) {
  const bucketSeconds = minutes <= 60 ? 60 : minutes <= 360 ? 300 : 900;
  return useQuery({
    queryKey: keys.telemetry(id, minutes),
    queryFn: ({ signal }) =>
      getJson<TelemetryBucket[]>(
        `/api/v1/terminals/${encodeURIComponent(id)}/telemetry${qs({ minutes, bucketSeconds })}`,
        signal,
      ),
    refetchInterval: 30_000,
  });
}

export interface VersionedConfig {
  config: DeviceConfig;
  etag: string;
}

export function useDeviceConfig(id: string) {
  return useQuery({
    queryKey: keys.config(id),
    queryFn: async ({ signal }): Promise<VersionedConfig> => {
      const res = await request<DeviceConfig>(`/api/v2/terminals/${encodeURIComponent(id)}/config`, { signal });
      return { config: res.data, etag: res.etag ?? `"${res.data.revision}"` };
    },
  });
}

export function useSaveDeviceConfig(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ update, etag }: { update: DeviceConfigUpdate; etag: string }) => {
      const res = await request<DeviceConfig>(`/api/v2/terminals/${encodeURIComponent(id)}/config`, {
        method: "PUT",
        body: update,
        ifMatch: etag,
      });
      return { config: res.data, etag: res.etag ?? `"${res.data.revision}"` };
    },
    onSuccess: (saved) => {
      qc.setQueryData(keys.config(id), saved);
      void qc.invalidateQueries({ queryKey: keys.configHistory(id) });
    },
  });
}

export function useConfigHistory(id: string) {
  return useQuery({
    queryKey: keys.configHistory(id),
    queryFn: ({ signal }) =>
      getJson<ConfigHistoryEntry[]>(`/api/v1/terminals/${encodeURIComponent(id)}/config/history`, signal),
  });
}

export function usePatchTerminal(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: TerminalPatch) =>
      request<TerminalDetail>(`/api/v1/terminals/${encodeURIComponent(id)}`, { method: "PATCH", body: patch }).then(
        (r) => r.data,
      ),
    onSuccess: (detail) => {
      qc.setQueryData(keys.terminal(id), detail);
      void qc.invalidateQueries({ queryKey: ["terminals"] });
      void qc.invalidateQueries({ queryKey: ["sales"] });
      void qc.invalidateQueries({ queryKey: ["fleet"] });
      void qc.invalidateQueries({ queryKey: keys.config(id) });
    },
  });
}

export interface AlertFilters {
  state: "open" | "resolved" | "all";
  severity?: string;
  terminalId?: string;
  limit?: number;
}

export function useAlerts(filters: AlertFilters) {
  return useQuery({
    queryKey: keys.alerts(filters),
    queryFn: ({ signal }) => getJson<Alert[]>(`/api/v1/alerts${qs({ ...filters })}`, signal),
    refetchInterval: LIVE,
  });
}

export function useAcknowledgeAlert() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, by, note }: { id: number; by: string; note?: string }) =>
      request<Alert>(`/api/v1/alerts/${id}/acknowledge`, { method: "POST", body: { by, note } }).then((r) => r.data),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["alerts"] });
    },
  });
}

export function useSalesSummary() {
  return useQuery({
    queryKey: keys.sales,
    queryFn: ({ signal }) => getJson<SalesSummary>("/api/v1/sales/summary", signal),
    refetchInterval: SLOW,
  });
}

export function useUpsellCandidates() {
  return useQuery({
    queryKey: keys.upsell,
    queryFn: ({ signal }) => getJson<UpsellCandidate[]>("/api/v1/sales/upsell-candidates?limit=25", signal),
    refetchInterval: SLOW,
  });
}

export interface CustomerFilters {
  q?: string;
  segment?: string;
  offset: number;
}

export function useCustomers(filters: CustomerFilters) {
  return useQuery({
    queryKey: keys.customers(filters),
    queryFn: async ({ signal }) => {
      const res = await request<CustomerSummary[]>(`/api/v1/customers${qs({ ...filters, limit: 50 })}`, { signal });
      return { items: res.data, total: res.total ?? res.data.length };
    },
    placeholderData: keepPreviousData,
  });
}

export function useCustomer(id: number) {
  return useQuery({
    queryKey: keys.customer(id),
    queryFn: ({ signal }) => getJson<CustomerDetail>(`/api/v1/customers/${id}`, signal),
  });
}

export function usePlans() {
  return useQuery({
    queryKey: keys.plans,
    queryFn: ({ signal }) => getJson<Plan[]>("/api/v1/plans", signal),
    staleTime: Infinity,
  });
}

export function useProvisionTerminal() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ProvisionRequest) =>
      request<TerminalDetail>("/api/v1/terminals", { method: "POST", body: req }).then((r) => r.data),
    onSuccess: (t) => {
      void qc.invalidateQueries({ queryKey: keys.customer(t.customer.id) });
      void qc.invalidateQueries({ queryKey: ["sales"] });
      void qc.invalidateQueries({ queryKey: ["customers"] });
    },
  });
}
