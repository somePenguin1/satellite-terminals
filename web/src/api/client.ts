import { currentOperator } from "../lib/operator";
import type { Problem } from "./types";

export class ApiError extends Error {
  readonly status: number;
  readonly problem: Problem;

  constructor(status: number, problem: Problem) {
    super(problem.detail ?? problem.title ?? `Request failed with status ${status}`);
    this.name = "ApiError";
    this.status = status;
    this.problem = problem;
  }

  /** The resource changed since it was read (optimistic concurrency failure). */
  get isConflict(): boolean {
    return this.status === 412 || this.status === 409;
  }
}

export interface ApiResponse<T> {
  data: T;
  etag: string | null;
  total: number | null;
}

interface RequestOptions {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  body?: unknown;
  ifMatch?: string;
  signal?: AbortSignal;
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<ApiResponse<T>> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  if (opts.ifMatch) headers["If-Match"] = opts.ifMatch;
  if (opts.method && opts.method !== "GET") headers["X-Actor"] = currentOperator();

  const res = await fetch(path, {
    method: opts.method ?? "GET",
    headers,
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
    signal: opts.signal,
  });

  if (!res.ok) {
    let problem: Problem = { status: res.status, title: res.statusText };
    try {
      problem = { ...problem, ...((await res.json()) as Problem) };
    } catch {
      // Non-JSON error body (e.g. a proxy error page); keep the status line.
    }
    throw new ApiError(res.status, problem);
  }

  const total = res.headers.get("X-Total-Count");
  const data = res.status === 204 ? (undefined as T) : ((await res.json()) as T);
  return { data, etag: res.headers.get("ETag"), total: total === null ? null : Number(total) };
}

export async function getJson<T>(path: string, signal?: AbortSignal): Promise<T> {
  return (await request<T>(path, { signal })).data;
}

/** Builds a query string, skipping empty values. */
export function qs(params: Record<string, string | number | undefined | null>): string {
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== "") sp.set(k, String(v));
  }
  const s = sp.toString();
  return s ? `?${s}` : "";
}
