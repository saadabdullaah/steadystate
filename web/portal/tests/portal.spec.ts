import { expect, test } from "@playwright/test";
import axe from "axe-core";
import { createServer, Server } from "node:http";
import { readFile } from "node:fs/promises";
import { extname, resolve } from "node:path";

const catalog = { tenants: [{ name: "payments", teamPath: "gitops/teams/payments", owners: ["platform-team"], lifecycle: "Active", applications: [{ name: "demo", lifecycle: "Active" }], databases: [{ name: "orders", lifecycle: "Active" }], services: [{ name: "xyz", template: "full-stack", version: "v0.1.1", components: ["web", "api"], lifecycle: "Active" }] }] };
const summary = { kind: "Application", namespace: "team-payments", name: "demo", phase: "Healthy", ready: "True", generation: 2, observedGeneration: 2 };
let server: Server;

test.beforeAll(async () => {
  const root = resolve(process.cwd(), "../../internal/platformctl/portalassets");
  const types: Record<string,string> = { ".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8" };
  server = createServer(async (request, response) => {
    const relative = request.url === "/" ? "index.html" : request.url?.slice(1).split("?", 1)[0] || "index.html";
    const safe = /^[a-zA-Z0-9._-]+$/.test(relative) ? relative : "index.html";
    try { const content = await readFile(resolve(root, safe)); response.writeHead(200, { "Content-Type": types[extname(safe)] ?? "application/octet-stream" }); response.end(content); }
    catch { const content = await readFile(resolve(root, "index.html")); response.writeHead(200, { "Content-Type": types[".html"] }); response.end(content); }
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
      readiness: { state: "Ready", checks: [{ name: "GitHub authentication", status: "Pass", details: "Authenticated", remediation: "gh auth login" }] }
    };
    if (path === "events") return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: heartbeat\ndata: {}\n\n" });
    const response = data[path] ?? (path === "teams/payments" ? { catalog: catalog.tenants[0], namespace: "team-payments", status: summary } : { summary, status: { phase: "Healthy", activeVersion: "v1.0.0", resolvedGitRevision: "a".repeat(40), conditions: [{ type: "Ready", status: "True", message: "Serving" }] }, generation: 2 });
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ apiVersion: "portal.steadystate.dev/v1alpha1", observedAt: "2026-08-11T00:00:00Z", data: response }) });
  });
}

test.beforeEach(async ({ page }) => { await mockPortal(page); });

test("portal exposes an accessible operational overview", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Everything steady, in one place" })).toBeVisible();
  await expect(page.getByText("Healthy", { exact: true }).first()).toBeVisible();
  await page.addScriptTag({ content: axe.source });
  const results = await page.evaluate(async () => await (window as any).axe.run());
  expect(results.violations.filter(item => ["serious", "critical"].includes(item.impact ?? ""))).toEqual([]);
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
});

test("keyboard shortcut focuses catalog search", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Everything steady, in one place" })).toBeVisible();
  await page.keyboard.press("Control+K");
  await expect(page.getByLabel("Search catalog")).toBeFocused();
});
