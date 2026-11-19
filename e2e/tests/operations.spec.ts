import { expect, test } from "@playwright/test";

test("fleet overview shows health, regions and the live ingest rate", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Fleet overview" })).toBeVisible();
  await expect(page.getByText("Live feed connected")).toBeVisible();
  await expect(page.getByText("reports/s ingested")).toBeVisible();

  const regionRows = page.locator("table.data tbody tr");
  await expect(regionRows).toHaveCount(7);

  const degraded = page.getByRole("button", { name: /Degraded/ });
  await degraded.click();
  await expect(degraded).toHaveAttribute("aria-pressed", "true");
});

test("operator finds a terminal and watches it stream live", async ({ page }) => {
  await page.goto("/terminals");
  await page.getByLabel("Search").fill("SAT-00001");
  await page.getByRole("row", { name: /SAT-00001/ }).click();
  await expect(page.getByRole("heading", { name: /SAT-00001/ })).toBeVisible();
  // The C agent reports every few seconds; the page subscribes over the live WebSocket.
  await expect(page.getByText(/Streaming live/)).toBeVisible({ timeout: 30_000 });
});

test("operator changes a terminal's configuration and sees the revision history", async ({ page }) => {
  await page.goto("/terminals/SAT-00003");
  const interval = page.getByLabel("Report every");
  await expect(interval).toBeVisible();
  await interval.fill("10");
  await page.getByRole("button", { name: "Save configuration" }).click();
  await expect(page.getByText(/Configuration saved as revision \d+/)).toBeVisible();
  await expect(page.getByText(/report interval ms 5000 to 10000/)).toBeVisible();
});

test("concurrent edits surface a conflict instead of overwriting", async ({ page, request }) => {
  await page.goto("/terminals/SAT-00005");
  await expect(page.getByLabel("Report every")).toBeVisible();

  // Someone else saves first, through the API.
  const url = "/api/v1/terminals/SAT-00005/config";
  const res = await request.get(url);
  await request.put(url, {
    headers: { "If-Match": res.headers()["etag"]! },
    data: { ...(await res.json()), txPowerDbm: 25 },
  });

  await page.getByLabel("Report every").fill("7");
  await page.getByRole("button", { name: "Save configuration" }).click();
  await expect(page.getByRole("alert")).toContainText("Someone else changed this configuration");
  await page.getByRole("button", { name: "Reload configuration" }).click();
  await expect(page.getByText(/Revision \d+, changed by/)).toBeVisible();
});

test("alerts can be acknowledged", async ({ page }) => {
  await page.goto("/alerts");
  await expect(page.getByRole("heading", { name: "Alerts" })).toBeVisible();
  const ack = page.getByRole("button", { name: "Acknowledge" }).first();
  if (await ack.isVisible().catch(() => false)) {
    await ack.click();
    await expect(page.getByText(/Acknowledged by ops-oncall/).first()).toBeVisible();
  } else {
    await expect(page.getByText(/No open alerts|Every open alert has an owner/)).toBeVisible();
  }
});
