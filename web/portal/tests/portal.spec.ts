import { expect, test } from "@playwright/test";
import axe from "axe-core";
import { createServer, Server } from "node:http";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

const catalog = { tenants: [{ name: "payments", teamPath: "gitops/teams/payments", owners: ["platform-team"], lifecycle: "Active", applications: [{ name: "demo", lifecycle: "Active" }], databases: [{ name: "orders", lifecycle: "Active" }], services: [{ name: "xyz", template: "full-stack", version: "v0.1.1", components: ["web", "api"], lifecycle: "Active" }] }] };
const summary = { kind: "Application", namespace: "team-payments", name: "demo", phase: "Healthy", ready: "True", generation: 2, observedGeneration: 2 };
let server: Server;

test.beforeAll(async () => {
  const root = resolve(process.cwd(), "../../internal/platformctl/portalassets");
  const indexAsset = { path: resolve(root, "index.html"), contentType: "text/html; charset=utf-8" };
  const scriptAsset = { path: resolve(root, "app.js"), contentType: "text/javascript; charset=utf-8" };
  const styleAsset = { path: resolve(root, "app.css"), contentType: "text/css; charset=utf-8" };
  server = createServer(async (request, response) => {
    const relative = request.url === "/" ? "index.html" : request.url?.slice(1).split("?", 1)[0] || "index.html";
    let asset = indexAsset;
    if (relative === "app.js") asset = scriptAsset;
    if (relative === "app.css") asset = styleAsset;
    try { const content = await readFile(asset.path); response.writeHead(200, { "Content-Type": asset.contentType }); response.end(content); }
    catch { const content = await readFile(indexAsset.path); response.writeHead(200, { "Content-Type": indexAsset.contentType }); response.end(content); }
  });
  await new Promise<void>((resolveReady, reject) => { server.once("error", reject); server.listen(4173, "127.0.0.1", resolveReady); });
});
test.afterAll(async () => { if (server) await new Promise<void>((resolveClosed, reject) => server.close(error => error ? reject(error) : resolveClosed())); });

async function mockPortal(page: import("@playwright/test").Page) {
  await page.route("**/api/v1/**", async route => {
    const path = new URL(route.request().url()).pathname.replace("/api/v1/", "");
    const data: Record<string, unknown> = {
      meta: { build: { version: "v1.0.0", portalVersion: "v1.0.0", portalAssetsDigest: `sha256:${"a".repeat(64)}` }, context: "acceptance", profile: "full", repository: "saadabdullaah/steadystate", csrfToken: "fixture", mode: "local-owner", links: {} },
      overview: { context: "acceptance", profile: "full", repository: "saadabdullaah/steadystate", counts: { teams: 1, services: 1, applications: 1, databases: 1 }, health: "Healthy", resources: [summary] },
      catalog,
      teams: catalog.tenants,
      services: [{ team: "payments", service: catalog.tenants[0].services[0] }],
      requests: [{ databaseId: 1, displayTitle: "service.scaffold fixture", status: "completed", conclusion: "success", url: "https://github.com/saadabdullaah/steadystate/actions", createdAt: "2026-08-11T00:00:00Z" }],
      readiness: { state: "Ready", checks: [{ name: "GitHub authentication", status: "Pass", details: "Authenticated", remediation: "gh auth login" }] },
      "teams/payments/applications/demo/rollout": { strategy: "canary", status: { phase: "Progressing", currentStepIndex: 1, readyReplicas: 2 }, analyses: [{ name: "demo-analysis-2", status: { phase: "Successful", message: "All metric gates passed" } }] },
      "teams/payments/applications/demo/slo": { status: "success", data: { result: [{ value: [1, "12.75"] }] } },
      "teams/payments/applications/demo/logs": { status: "success", data: { result: [{ stream: { application: "demo" }, values: [["1786406400000000000", JSON.stringify({ level: "info", message: "request complete", request_id: "request-1" })]] }] } },
      "teams/payments/applications/demo/traces": { traces: [{ traceID: "trace-1", rootTraceName: "GET /", rootServiceName: "demo", durationMs: 18 }] },
      "teams/payments/applications/demo/policy": [{ name: "demo-policy", status: { summary: { pass: 4, fail: 0 } } }],
      "teams/payments/applications/demo/doctor": [{ name: "GitOps revision", status: "Pass", details: "Catalog and Argo revisions agree", remediation: "platformctl request status" }]
    };
    if (path === "events") return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: heartbeat\ndata: {}\n\n" });
    const response = data[path] ?? (path === "teams/payments" ? { catalog: catalog.tenants[0], namespace: "team-payments", status: summary } : { summary, status: { phase: "Healthy", activeVersion: "v1.0.0", resolvedGitRevision: "a".repeat(40), conditions: [{ type: "Ready", status: "True", message: "Serving" }] }, generation: 2 });
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ apiVersion: "portal.steadystate.dev/v1alpha1", observedAt: "2026-08-11T00:00:00Z", data: response }) });
  });
}

test.beforeEach(async ({ page }) => { await mockPortal(page); });

test("portal exposes an accessible operational overview", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "The platform is steady." })).toBeVisible();
  await expect(page.getByText("Healthy", { exact: true }).first()).toBeVisible();
  await page.addScriptTag({ content: axe.source });
  const results = await page.evaluate(async () => await (window as any).axe.run());
  expect(results.violations.filter(item => ["serious", "critical"].includes(item.impact ?? ""))).toEqual([]);
  if (process.env.PORTAL_SCREENSHOT_PATH) await page.screenshot({ path: process.env.PORTAL_SCREENSHOT_PATH, fullPage: true });
});

test("primary navigation stays usable on mobile and dark theme", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await page.getByLabel("Color theme").selectOption("dark");
  await page.getByRole("button", { name: "Teams" }).click();
  await expect(page.getByRole("heading", { name: "Teams" })).toBeVisible();
  await page.getByRole("button", { name: /payments/ }).click();
  await expect(page.getByRole("heading", { name: "payments" })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.getByRole("button", { name: "Requests" }).click();
  await expect(page.getByRole("button", { name: "Requests" })).toHaveCSS("background-color", "rgb(240, 239, 232)");
  await expect(page.getByRole("button", { name: "Requests" })).toHaveCSS("color", "rgb(21, 23, 28)");
  await page.addScriptTag({ content: axe.source });
  const results = await page.evaluate(async () => await (window as any).axe.run());
  expect(results.violations.filter(item => ["serious", "critical"].includes(item.impact ?? ""))).toEqual([]);
  if (process.env.PORTAL_MOBILE_SCREENSHOT_PATH) await page.screenshot({ path: process.env.PORTAL_MOBILE_SCREENSHOT_PATH, fullPage: true });
});

test("keyboard shortcut focuses catalog search", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "The platform is steady." })).toBeVisible();
  await page.keyboard.press("Control+K");
  await expect(page.getByLabel("Search catalog")).toBeFocused();
});

test("primary product surfaces preserve the editorial system", async ({ page }) => {
  const surfaces = [
    ["/services", "Services", "services"],
    ["/changes", "Create a reviewed change", "changes"],
    ["/requests", "Requests", "requests"],
    ["/readiness", "Readiness", "readiness"]
  ] as const;
  for (const [path, heading, slug] of surfaces) {
    await page.goto(path);
    await expect(page.getByRole("heading", { name: heading })).toBeVisible();
    if (process.env.PORTAL_GALLERY_DIR) await page.screenshot({ path: resolve(process.env.PORTAL_GALLERY_DIR, `${slug}.png`), fullPage: true });
  }
});

test("application diagnosis is curated instead of exposing raw objects", async ({ page }) => {
  await page.goto("/applications/payments/demo");
  await page.getByLabel("Color theme").selectOption("dark");
  await page.getByRole("tab", { name: "Doctor" }).click();
  await expect(page.getByRole("heading", { name: "Dependency path" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Doctor" })).toHaveCSS("background-color", "rgb(111, 143, 255)");
  await expect(page.getByRole("tab", { name: "Doctor" })).toHaveCSS("color", "rgb(12, 16, 32)");
  await page.addScriptTag({ content: axe.source });
  const results = await page.evaluate(async () => await (window as any).axe.run());
  expect(results.violations.filter(item => ["serious", "critical"].includes(item.impact ?? ""))).toEqual([]);
  await expect(page.locator("pre")).toHaveCount(0);
});

test("application operations use native summaries across every signal", async ({ page }) => {
  await page.goto("/applications/payments/demo");
  const expected = new Map([
    ["Rollout", "Progressive delivery"],
    ["Slo", "Five-minute request signal"],
    ["Logs", "Recent application logs"],
    ["Traces", "Distributed traces"],
    ["Policy", "Policy and provenance"],
    ["Doctor", "Dependency path"]
  ]);
  for (const [tab, heading] of expected) {
    await page.getByRole("tab", { name: tab }).click();
    await expect(page.getByRole("heading", { name: heading })).toBeVisible();
  }
  await expect(page.locator("pre")).toHaveCount(0);
});
