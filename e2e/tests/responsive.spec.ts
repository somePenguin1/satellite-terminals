import { expect, test } from "@playwright/test";

for (const path of ["/", "/terminals", "/terminals/SAT-00001", "/sales", "/customers"]) {
  test(`no horizontal overflow on ${path} at phone width`, async ({ page }) => {
    await page.goto(path);
    await expect(page.locator("main h1").first()).toBeVisible();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(1);
    await expect(page.getByRole("navigation", { name: "Main" })).toBeVisible();
  });
}
