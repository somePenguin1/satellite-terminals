# Satellite Terminal Fleet Management Platform

A full-stack platform for operating a fleet of broadband satellite terminals. It ingests
link telemetry from thousands of devices, keeps live health and alerting for the operations
team, and turns usage data into revenue insight for the sales team.

| Layer | Tech | What it does |
|---|---|---|
| Device agent | **C** (C11, no dependencies) | Runs on each terminal; measures link health, latency, jitter, loss, throughput; streams JSON over WebSocket; pulls config over REST with ETags |
| Ingest | **Go**, **gRPC**, **WebSockets** | Accepts telemetry from agents (WebSocket) and fleet gateways/simulators (gRPC streams), batches it into Postgres with `COPY`, maintains rollups, fans live data out to browsers |
| Control plane | **Java 21**, Spring Boot | Versioned REST APIs (`/api/v1`, `/api/v2`) for terminals, device configuration, alerts and sales analytics; owns the schema (Flyway) |
| Storage | **PostgreSQL** 16 | Relational fleet/commercial model plus day-partitioned telemetry and write-time rollups |
| UI | **React** + **TypeScript** | Responsive console with operations and sales workflows, live updates over WebSocket |
| Delivery | GitHub Actions, Jenkins, Docker, Kubernetes | Unit, integration, end-to-end and load tests on every PR; images to GHCR; deploys to Kubernetes |

```mermaid
flowchart LR
    subgraph Devices
        A[C agent<br/>SAT-00001..2] -- "WebSocket JSON" --> I
        S[Fleet simulator<br/>~4,750 terminals] -- "gRPC client streams" --> I
        A -. "GET /api/v1/terminals/{id}/config<br/>If-None-Match" .-> J
    end
    I[Go ingest<br/>validate · batch · COPY] -- "COPY + upsert" --> P[(PostgreSQL<br/>telemetry partitions<br/>terminal_status · rollups)]
    J[Java fleet API<br/>REST v1/v2 · alerts · maintenance] <--> P
    W[React console] -- "/api" --> J
    W <-- "/ws/live ticks + subscribed terminals" --> I
```

## Quick start

Requires Docker.

```bash
docker compose up --build
```

| | |
|---|---|
| Console | http://localhost:3000 |
| REST API docs (Swagger UI) | http://localhost:8080/swagger-ui.html |
| Ingest metrics (Prometheus) | http://localhost:8081/metrics |

On start, Flyway seeds a deterministic demo fleet: **5,000 terminals**, 800 customers, 7 plans.
The simulator mirrors that fleet from the API and streams telemetry for ~4,750 in-service
terminals over gRPC (rain fade, obstructions, evening congestion, occasional outages). Two real
C agents report for `SAT-00001` and `SAT-00002` over WebSocket.

Things to try:

- **Operations → Overview:** health across the fleet map; click a dot to open the terminal.
- **Terminal page:** live readings stream in; change *Report every* and save. The C agent picks
  up the new interval within 30 seconds. Open the same terminal in two tabs and save in both to
  see the conflict handling.
- **Sales → Customers:** order a terminal, then press *Activate service*. Within a minute the
  simulator brings it online and it turns healthy on the map.
- **Sales → Revenue:** recurring revenue, plus upgrade suggestions for terminals running near
  their plan's limit.

## Repository layout

```
agent/              C telemetry agent: WebSocket client, HTTP/ETag config, link probes, tests
services/ingest/    Go: gRPC + WebSocket ingest, batching pipeline, rollups, live hub, simulator, load tester
services/fleet-api/ Java/Spring Boot: REST v1/v2, alert evaluator, partition maintenance, Flyway schema
web/                React + TypeScript console (Vite)
proto/              gRPC contract (fleet.telemetry.v1)
e2e/                Playwright end-to-end tests (browser + API)
deploy/k8s/         Kustomize base + overlays (ci, staging)
.github/workflows/  CI (tests on every PR) and CD (images → kind smoke deploy → staging)
Jenkinsfile         The same pipeline for Jenkins
docs/               Architecture and design decisions
```

## APIs

**gRPC** (`proto/fleet/telemetry/v1/telemetry.proto`): `TelemetryIngest.StreamTelemetry` (client
stream, acked on close) and `SendBatch` (store-and-forward backlog). Optional bearer-token auth,
health service and reflection (`grpcurl -plaintext localhost:9090 list`).

**WebSocket** (ingest service): `/ws/ingest` for device agents (JSON reports, with rejection
reasons sent back) and `/ws/live` for browsers (1 Hz fleet ticks, plus every report for subscribed
terminals).

**REST** (fleet API), versioned by path:

| Endpoint | Notes |
|---|---|
| `GET /api/v1/terminals`, `GET /api/v2/terminals` | v1: flat items, offset paging, `X-Total-Count`. v2: nested items with live metrics, opaque keyset cursor |
| `GET/PATCH /api/v1/terminals/{id}`, `POST /api/v1/terminals` | Detail, lifecycle (activate/suspend/resume/decommission), plan changes, ordering |
| `GET/PUT /api/v1/terminals/{id}/config` | Flat config, interval in ms |
| `GET/PUT /api/v2/terminals/{id}/config` | Grouped config, interval in s. Both versions read and write the same record |
| `GET /api/v1/terminals/{id}/config/history` | Every revision, recorded by a database trigger |
| `GET /api/v1/terminal-configs` | Bulk keyset export used by provisioning tools and the simulator |
| `GET /api/v1/fleet/{summary,map,timeseries}`, `GET /api/v1/terminals/{id}/telemetry` | Ops read models |
| `GET /api/v1/alerts`, `POST /api/v1/alerts/{id}/acknowledge` | Triage queue |
| `GET /api/v1/sales/{summary,upsell-candidates}`, `GET /api/v1/customers[/{id}]`, `GET /api/v1/plans` | Sales read models |

Config reads return `ETag: "<revision>"` and honour `If-None-Match` (agents poll cheaply and get
`304`). Writes require `If-Match`: `428` without it, `412` if someone saved first. Errors are
RFC 7807 problem details, with per-field messages for validation failures.

## Testing

| Component | Unit | Integration | |
|---|---|---|---|
| C agent | 94 checks: SHA-1/base64 vectors, RFC 6455 frames and accept key, HTTP chunked parsing, probe math, ring buffer, link model, loopback WebSocket handshake | also run under ASan + UBSan, and against musl in the image build | `make -C agent test asan` |
| Go ingest | validation, pipeline batching/ordering/backpressure/retries, rollups, live hub, gRPC over bufconn, WebSocket over httptest; all with `-race` | COPY/upsert/rollups against real Postgres and the real migrations | `go test -race ./...`, `-tags=integration` |
| Java API | ETag/cursor/mapper/lifecycle logic, `@WebMvcTest` HTTP contract | `@SpringBootTest` + Testcontainers Postgres: pagination, concurrency, lifecycle, alerts, sales | `mvn verify` |
| Web | Vitest + Testing Library: API client, live feed reconnect/resubscribe, config editor validation and conflicts | | `npm test` |
| System | Playwright against the full Docker stack: data path, config reaching the C agent, UI workflows, phone-width layout | load test gate: ≥ 5,000 persisted reports/s | `make e2e`, `make loadtest` |

## Performance

`cmd/loadtest` drives the ingest service over gRPC and measures throughput **from the service's own
`ingest_reports_persisted_total` counter**, i.e. rows durably committed to Postgres, not messages
sent. CI runs it on every pull request at 6,000 reports/s for 60 s and fails the build below
5,000/s or on any write failure. The result is attached to each run's summary. See
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md#throughput) for the design choices behind it.

```bash
make up && make loadtest            # default: 6,000/s for 60 s
docker compose run --rm --no-deps --entrypoint /loadtest simulator \
  -target ingest:9090 -metrics http://ingest:8081/metrics -rate 0 -duration 30s   # find the ceiling
```

## CI/CD

- **CI** (`.github/workflows/ci.yml`, every PR and push): C (tests, sanitizers, cppcheck, static
  build), Go (tidy/codegen drift check, vet, staticcheck, race tests, Postgres integration tests),
  Java (`mvn verify` with Testcontainers), web (lint, types, tests, build), then the full stack in
  Docker Compose for Playwright and the load-test gate.
- **CD** (`.github/workflows/cd.yml`, after CI passes on `main`): builds and pushes five images
  to GHCR tagged with the commit SHA, deploys exactly that SHA to a fresh **kind** Kubernetes
  cluster, and checks data really flows (WebSocket and gRPC paths). If `STAGING_ENABLED` is set,
  it then rolls the same SHA out to a staging cluster (`STAGING_KUBECONFIG` secret, protected
  `staging` environment).
- **Jenkins** (`Jenkinsfile`): the same stages for on-prem CI, with parallel component builds.

## Configuration

| Service | Variable | Default |
|---|---|---|
| ingest | `DATABASE_URL` | `postgres://fleet:fleet@localhost:5432/fleet` |
| | `GRPC_ADDR`, `HTTP_ADDR` | `:9090`, `:8081` |
| | `INGEST_TOKEN` | empty (auth off) |
| | `PIPELINE_SHARDS`, `BATCH_SIZE`, `FLUSH_INTERVAL`, `QUEUE_SIZE` | `4`, `1000`, `100ms`, `20000` |
| fleet-api | `SPRING_DATASOURCE_URL` / `_USERNAME` / `_PASSWORD` | local `fleet` database |
| | `FLEET_TELEMETRY_RETENTION_DAYS` | `7` |
| | `FLEET_UPSELL_UTILIZATION_THRESHOLD` | `0.65` |
| agent | `TERMINAL_ID`, `INGEST_URL`, `FLEET_API_URL`, `INGEST_TOKEN`, `AGENT_MODE` (`sim`/`system`) | see `satfleet-agent --help` |
