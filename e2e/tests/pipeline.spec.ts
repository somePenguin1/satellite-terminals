import { expect, test } from "@playwright/test";

// Data-path checks through the public API: device -> ingest (Go) -> Postgres -> fleet API (Java).

type Detail = { metrics: { transport: string | null; lastSeenAt: string | null } };

async function terminal(request: import("@playwright/test").APIRequestContext, id: string): Promise<Detail> {
  const res = await request.get(`/api/v1/terminals/${id}`);
  expect(res.ok()).toBeTruthy();
  return res.json();
}

test("C agent reports over WebSocket and the simulator over gRPC", async ({ request }) => {
  await expect
    .poll(async () => (await terminal(request, "SAT-00001")).metrics.transport, { timeout: 60_000 })
    .toBe("ws");
  await expect
    .poll(async () => (await terminal(request, "SAT-00010")).metrics.transport, { timeout: 60_000 })
    .toBe("grpc");

  const agent = await terminal(request, "SAT-00001");
  const ageMs = Date.now() - Date.parse(agent.metrics.lastSeenAt!);
  expect(ageMs).toBeLessThan(30_000);
});

test("write-time rollups feed the fleet trend endpoint", async ({ request }) => {
  await expect
    .poll(
      async () => {
        const minutes: { samples: number }[] = await (await request.get("/api/v1/fleet/timeseries?minutes=10")).json();
        return minutes.reduce((s, m) => s + m.samples, 0);
      },
      { timeout: 60_000 },
    )
    .toBeGreaterThan(1000);
});

test("a config change over REST changes the C agent's reporting cadence", async ({ request }) => {
  test.setTimeout(180_000);
  const url = "/api/v1/terminals/SAT-00002/config";
  const current = await request.get(url);
  const body = await current.json();
  const put = await request.put(url, {
    headers: { "If-Match": current.headers()["etag"]!, "X-Actor": "e2e" },
    data: { ...body, reportIntervalMs: 2000 },
  });
  expect(put.status()).toBe(200);

  // The agent re-reads config every 30 s (ETag/304 when unchanged). At a 2 s interval a full
  // minute holds ~30 samples; at the original 5 s it held ~12.
  await expect
    .poll(
      async () => {
        const buckets: { samples: number }[] = await (
          await request.get("/api/v1/terminals/SAT-00002/telemetry?minutes=3&bucketSeconds=60")
        ).json();
        return Math.max(0, ...buckets.slice(-2, -1).map((b) => b.samples));
      },
      { timeout: 150_000, intervals: [5_000] },
    )
    .toBeGreaterThanOrEqual(20);
});

test("stale writes are rejected with 412 and missing If-Match with 428", async ({ request }) => {
  const url = "/api/v2/terminals/SAT-00004/config";
  const first = await request.get(url);
  const cfg = await first.json();
  const update = { reporting: { intervalSeconds: 5, level: "detailed" }, radio: cfg.radio, network: cfg.network, firmware: cfg.firmware };

  expect((await request.put(url, { data: update })).status()).toBe(428);
  expect((await request.put(url, { data: update, headers: { "If-Match": first.headers()["etag"]! } })).status()).toBe(200);
  expect((await request.put(url, { data: update, headers: { "If-Match": first.headers()["etag"]! } })).status()).toBe(412);
});
