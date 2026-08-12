import { expect, test } from "@playwright/test";
import axe from "axe-core";
import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

const launchURL = process.env.PORTAL_LAUNCH_URL;
const artifactRoot = resolve(process.cwd(), "../../.artifacts/phase9/acceptance");

async function runAxe(page: import("@playwright/test").Page) {
  await page.addScriptTag({ content: axe.source });
  const result = await page.evaluate(async () => await (window as any).axe.run());
  expect(result.violations.filter((item: any) => ["serious", "critical"].includes(item.impact ?? ""))).toEqual([]);
  return result;
}

test.describe("real Phase 9 portal", () => {
  test.describe.configure({ retries: 0 });
  test.skip(!launchURL, "PORTAL_LAUNCH_URL is required for hosted acceptance");

  test("reads the full platform and submits one reviewed proposal", async ({ page }) => {
    mkdirSync(resolve(artifactRoot, "screenshots"), { recursive: true });
    const startedAt = Date.now();
    await page.goto(launchURL!);
    await expect(page).toHaveURL(/http:\/\/127\.0\.0\.1:\d+\/$/);
    await expect(page.getByRole("heading", { name: "Everything steady, in one place" })).toBeVisible({ timeout: 30_000 });
    const initialRenderMilliseconds = Date.now() - startedAt;
    expect(initialRenderMilliseconds).toBeLessThanOrEqual(2_000);
    await expect(page.locator(".live")).toContainText("Live", { timeout: 5_000 });
    await page.screenshot({ path: resolve(artifactRoot, "screenshots/01-overview-light.png") });

    await page.getByLabel("Color theme").selectOption("dark");
    await page.screenshot({ path: resolve(artifactRoot, "screenshots/02-overview-dark.png") });
    await page.getByRole("button", { name: "Readiness" }).click();
    await expect(page.getByRole("heading", { name: "Readiness" })).toBeVisible();
    await page.screenshot({ path: resolve(artifactRoot, "screenshots/03-readiness.png") });

    await page.getByRole("button", { name: "Teams", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Teams" })).toBeVisible();
    await page.getByRole("button", { name: "Services", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Services" })).toBeVisible();
    await page.getByRole("button", { name: "Requests", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Requests" })).toBeVisible();

    const accessibility: Record<string, unknown> = {};
    accessibility.requests = await runAxe(page);

    await page.goto(new URL("/applications/xyz/xyz-api", launchURL!).toString());
    await expect(page.getByRole("heading", { name: "xyz-api" })).toBeVisible();
    await expect(page.locator(".detail-grid")).toBeVisible({ timeout: 30_000 });
    await page.screenshot({ path: resolve(artifactRoot, "screenshots/04-application.png") });
    for (const tab of ["Rollout", "Slo", "Logs", "Traces", "Policy", "Doctor"]) {
      await page.getByRole("tab", { name: tab }).click();
      await expect(page.locator(".data-view")).toBeVisible({ timeout: 30_000 });
    }
    accessibility.application = await runAxe(page);

    await page.goto(new URL("/databases/xyz/xyz", launchURL!).toString());
    await expect(page.getByRole("heading", { name: "xyz" })).toBeVisible();
    await expect(page.locator(".data-view")).toBeVisible({ timeout: 30_000 });
    await page.getByRole("tab", { name: "Backups" }).click();
    await expect(page.locator(".data-view")).toBeVisible({ timeout: 30_000 });
    await page.screenshot({ path: resolve(artifactRoot, "screenshots/05-database-backups.png") });
    accessibility.database = await runAxe(page);

    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(new URL("/overview", launchURL!).toString());
    await expect(page.getByRole("heading", { name: "Everything steady, in one place" })).toBeVisible({ timeout: 30_000 });
    await page.screenshot({ path: resolve(artifactRoot, "screenshots/06-overview-mobile.png") });
    writeFileSync(resolve(artifactRoot, "accessibility.json"), `${JSON.stringify(accessibility, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });
    writeFileSync(resolve(artifactRoot, "browser-performance.json"), `${JSON.stringify({ initialRenderMilliseconds, initialRenderBudgetMilliseconds: 2_000, sseState: "Live" }, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.getByRole("button", { name: "Changes", exact: true }).click();
    await page.getByLabel("Operation").selectOption("team.create");
    const team = process.env.PHASE9_SMOKE_TEAM ?? "portal-smoke";
    await page.getByLabel("Resource name").fill(team);
    await page.getByLabel("Owner identity").fill("platform-team");
    await page.getByLabel("Allowed repository").fill("ghcr.io/saadabdullaah/steadystate-services");
    await page.getByRole("button", { name: "Generate deterministic plan" }).click();
    await expect(page.getByText("Review boundary")).toBeVisible();
    await expect(page.locator(".diff")).toContainText(team);
    await page.screenshot({ path: resolve(artifactRoot, "screenshots/07-reviewed-plan.png") });
    await page.getByRole("button", { name: "Submit through GitHub" }).click();
    const notice = page.getByRole("status").filter({ hasText: "dispatched" });
    await expect(notice).toBeVisible({ timeout: 75_000 });
    const result = await notice.textContent();
    writeFileSync(resolve(artifactRoot, "proposal-result.txt"), `${result ?? ""}\n`, { encoding: "utf8", mode: 0o600 });
  });
});
