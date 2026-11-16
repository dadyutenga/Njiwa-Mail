import { defineConfig, devices } from "@playwright/test";

// Browser end-to-end tests against a running stack (docker compose up -d: the
// dashboard on :3000 proxies /admin/v1 to the API). CI starts it in the `e2e`
// job; locally: E2E_BASE_URL=http://127.0.0.1:3000 E2E_EMAIL=… E2E_PASSWORD=… npm run e2e
export default defineConfig({
  testDir: "./e2e",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false, // the tests share one installation and one admin account
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
