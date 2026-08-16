import { defineConfig, devices } from "@playwright/test";

const allBrowsers = process.env.FULL_BROWSER_MATRIX === "true";

export default defineConfig({
  testDir: "./tests",
  timeout: 30_000,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  workers: allBrowsers ? 1 : undefined,
  reporter: process.env.CI ? [["html", { outputFolder: "../../.artifacts/phase9/playwright-report", open: "never" }], ["list"]] : "list",
  use: {
    baseURL: "http://127.0.0.1:4173",
    launchOptions: process.env.PLAYWRIGHT_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH } : undefined,
    trace: "retain-on-failure",
    video: process.env.PLAYWRIGHT_DISABLE_VIDEO === "true" ? "off" : process.env.RECORD_VIDEO === "true" ? "on" : "retain-on-failure",
    screenshot: "only-on-failure"
  },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 } } },
    ...(allBrowsers ? [
      { name: "firefox", use: { ...devices["Desktop Firefox"] } },
      { name: "webkit", use: { ...devices["Desktop Safari"] } }
    ] : [])
  ]
});
