# Architecture and design decisions

This document explains *why* the platform is built the way it is: the trade-offs, what was
deliberately left out, and what would change at larger scale.

## Service boundaries

The backend splits along **data plane vs. control plane**:

- **Go ingest (data plane)** handles high-volume, latency-sensitive, stateless work: accept,
  validate, batch, write. It holds no business rules beyond "is this a well-formed sample from
  a terminal we know?". It scales horizontally behind a headless Kubernetes service.
- **Java fleet API (control plane)** handles low-volume, rule-heavy work: lifecycle state
  machine, plan rules, optimistic concurrency on configuration, alerting, analytics. It owns
  the schema (Flyway), so there's exactly one place migrations come from.

They share Postgres rather than calling each other. This is a deliberate shortcut: one
database is the integration point and Postgres is the bottleneck we'd scale first anyway. The
seam is clean enough to put a broker (Kafka/NATS) between ingest and storage later without
touching the API.

## Telemetry path

```
agent/simulator ─▶ front end (gRPC stream | WebSocket) ─▶ Service.Accept
   validate (shape, ranges, clock window) ─▶ registry lookup (known, not decommissioned)
   ─▶ pipeline.Submit (shard by terminal ID) ─▶ shard batcher ─▶ Postgres tx:
        COPY telemetry  +  set-based upsert of terminal_status
   ─▶ on success: rollup aggregator (minute×region, hour×terminal) ─▶ flushed every 5 s
   ─▶ live hub: subscribed browsers get the report immediately
```

### Throughput

The design target is ≥ 5,000 reports/s sustained *persisted*, with headroom.

- **Batching with `COPY`.** One `COPY` of up to 1,000 rows per round trip instead of 1,000
  `INSERT`s. Batches flush when full or after 100 ms, so latency stays bounded at low traffic.
- **Set-based latest-state upsert.** `terminal_status` is updated with one
  `INSERT … SELECT FROM unnest($1::text[], …) ON CONFLICT DO UPDATE`, deduplicated to the
  newest sample per terminal first (Postgres rejects touching the same row twice in one statement).
- **Sharding by terminal ID.** Each terminal always lands on the same shard goroutine, so its
  samples are written in order, and two concurrent transactions never upsert the same status
  row. That removes row-lock contention and deadlocks inside a replica. Across replicas, rows are
  upserted in sorted ID order so locks are always taken in the same order.
- **Backpressure, not buffering.** Shard queues are bounded. When Postgres slows down, `Submit`
  blocks, which blocks the gRPC handler, which stops reading the stream, so HTTP/2 flow control
  pushes back on the client. Memory stays flat instead of growing until the pod is OOM-killed.
- **No foreign key on `telemetry`.** Validation against an in-memory registry (an atomically
  swapped map, refreshed every 15 s) replaces a per-row FK check on the hottest table.
- **Partitioned by day.** Retention is `DROP TABLE` on old partitions (metadata only) instead
  of deleting millions of rows and vacuuming. The Java service pre-creates partitions three days
  ahead; a default partition catches anything unexpected.
- **`fillfactor = 70` on `terminal_status`.** Leaves room for HOT updates, since every report
  rewrites a row there.
- **Write-time rollups.** Dashboards read `fleet_minute_stats` (minutes × regions) and
  `terminal_usage_hourly` instead of scanning raw samples. Each replica aggregates in memory and
  flushes *additive* upserts, so totals stay correct with any number of replicas. If a flush
  fails, the drained aggregates are merged back and retried. If a pod crashes, up to 5 s of
  rollup data is lost; raw telemetry is not. That's an accepted trade-off for analytics.

Measured in CI with `cmd/loadtest`, which reads the service's own persisted counter, so the
number reflects committed rows rather than messages sent.

### Delivery semantics

At-least-once from the agent's perspective: the agent buffers up to an hour of reports in a ring
buffer while disconnected and replays them oldest first. The server may store a replayed
sample twice if the connection dropped after the write but before the agent noticed. Reports
carry `(terminal_id, seq, ts)`, so duplicates are identifiable at query time. We chose not to
enforce uniqueness on the hot path (a unique index on a partitioned 50M-row/day table is
expensive); a nightly dedup job would be the next step if it mattered.

`terminal_status` only moves forward (`WHERE last_seen_at <= EXCLUDED.last_seen_at`), so late
replays never make a terminal look like it went back in time.

## Why both gRPC and WebSockets?

- **gRPC client streams** suit *aggregators*: a gateway, a ground-station collector or the
  simulator multiplexes thousands of terminals over a few long-lived HTTP/2 streams with
  protobuf encoding and an ack on close. The headless service plus client-side `round_robin`
  spreads streams over replicas, and the simulator recycles streams every 5 minutes so new
  replicas pick up load.
- **WebSockets** suit *constrained devices*: the C agent needs one TCP connection, a 30-line
  handshake and text frames, with no HTTP/2, protobuf runtime or TLS stack in the agent itself
  (TLS terminates at the gateway). The same transport serves browsers for the live feed.

## Device configuration and API versioning

Configuration is a versioned document. Every write is a compare-and-set
(`UPDATE … WHERE version = :expected`), exposed over HTTP as `ETag`/`If-Match`:

- `428 Precondition Required` if a write doesn't say what it's based on,
- `412 Precondition Failed` if someone saved first; the UI offers *Reload*,
- `304 Not Modified` for `If-None-Match` reads, so 5,000 agents polling every 30 s cost almost
  nothing.

A trigger copies every committed version to `terminal_config_history`, so the audit trail
can't be bypassed by a code path that forgets to write it.

**v1 → v2** shows how breaking changes are introduced: v2 groups fields, switches the interval
from milliseconds to seconds and nests the maintenance window. Both versions map to and from
the same stored record (`ConfigMapper`), so v1 clients (like the C agent) and v2 clients (the UI)
can interleave writes safely. The terminal list follows the same pattern: v1 keeps offset paging
for existing integrations, and v2 uses an opaque keyset cursor that stays stable while rows are
being inserted.

## Alerting

Rules live in one enum as SQL predicates over a single view, `terminal_health_v`, which is also
what the API and dashboards use. "Healthy", "degraded" and "offline" therefore mean the same
thing everywhere. The evaluator runs every 15 s on every API replica; a transaction-scoped
`pg_try_advisory_xact_lock` ensures only one evaluates per cycle, with no external coordinator.
Opening is idempotent thanks to a partial unique index (one open alert per terminal and kind),
and alerts resolve on their own when the condition clears. "Offline" scales with each terminal's
configured interval (3×, at least 60 s), so a maritime terminal reporting every 10 s isn't
flagged by a threshold designed for 1 s reporters.

## The C agent

- No dependencies beyond libc: SHA-1 and base64 (for the WebSocket handshake), HTTP/1.1 with
  chunked decoding and a minimal JSON reader. It's ~125 KB statically linked against musl.
- Latency, jitter and loss come from timed TCP-connect probes to the gateway; jitter is the mean
  absolute difference of consecutive RTTs (as in RFC 3550). In `system` mode, throughput comes
  from `/proc/net/dev` deltas, and handles counter resets. In `sim` mode, a modem model supplies
  SNR, obstruction and plan-shaped throughput on top of the real probe. The model is the same as
  the Go simulator's, so both produce comparable data.
- Reconnects use exponential backoff with jitter, so a gateway restart doesn't trigger a
  synchronized reconnect storm from the fleet.
- Tested under AddressSanitizer and UBSan, and against both glibc and musl.

## Frontend

- **One shared WebSocket** with reference-counted terminal subscriptions, resubscribing
  automatically after reconnects. Exposed to React via `useSyncExternalStore`.
- **Server state in TanStack Query**, with refresh intervals matched to the data: ops views every
  10 s, commercial data every minute.
- **Workflows, not CRUD screens.** Ops: map → health filter → terminal → live readings →
  config with conflict handling → history. Sales: revenue → upgrade suggestion → one-click plan
  change; customer → order → activation.
- **Hand-written SVG/canvas charts.** The fleet map draws 5,000 points on a canvas with
  problem terminals drawn last and larger. Line charts are ~150 lines of SVG, which keeps the
  bundle small and the styling consistent.

## What I'd do next at larger scale

| Pressure | Change |
|---|---|
| > ~50k reports/s or multi-region | Put Kafka/NATS between ingest and storage; independent consumers for raw storage, rollups and alerting |
| Telemetry storage growth | TimescaleDB (compression, continuous aggregates) or ClickHouse for raw samples; keep Postgres for the relational model |
| Device trust | mTLS per device with certificates provisioned at manufacture, instead of a shared token |
| Alert noise | Hysteresis (open after N bad samples, close after M good ones) and per-customer maintenance windows |
| Config push latency | Server-pushed "config changed" message over the existing agent WebSocket instead of 30 s polling |
| Observability | OpenTelemetry tracing across ingest → DB, Grafana dashboards over the existing Prometheus metrics |
