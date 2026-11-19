import { expect, test } from "@playwright/test";

test("revenue page summarises recurring revenue by plan", async ({ page }) => {
  await page.goto("/sales");
  await expect(page.getByText(/monthly recurring revenue from/)).toBeVisible();
  await expect(page.locator(".bars").first().locator("li")).toHaveCount(7);
});

test("sales orders a terminal, operations activates it, and it starts reporting", async ({ page }) => {
  test.setTimeout(150_000);
  await page.goto("/customers");
  await page.getByLabel("Segment").selectOption("maritime");
  await page.locator("table.data tbody tr").first().click();
  await expect(page.getByRole("heading", { name: "Order another terminal" })).toBeVisible();

  await page.getByRole("button", { name: "Order terminal" }).click();
  await expect(page).toHaveURL(/\/terminals\/SAT-\d{5,}$/);
  await expect(page.getByText("Awaiting activation")).toBeVisible();

  await page.getByRole("button", { name: "Activate service" }).click();
  await expect(page.getByRole("button", { name: "Suspend service" })).toBeVisible();

  // The simulator re-syncs the provisioned fleet every minute, so the new terminal comes online.
  await expect(page.locator(".head-health")).toHaveText(/Healthy|Degraded/, { timeout: 120_000 });
});
