import { defineConfig } from "@playwright/test";

// docs.test is mapped to the local server so the page has a non-local origin, which is what
// decides whether "Open in ChatGPT" may link out with a prompt (see lib/ai-services.ts).
export default defineConfig({
  testDir: "e2e",
  timeout: 30_000,
  fullyParallel: true,
  reporter: [["list"]],
  use: {
    baseURL: "http://docs.test:4173",
    launchOptions: { args: ["--host-resolver-rules=MAP docs.test 127.0.0.1", "--unsafely-treat-insecure-origin-as-secure=http://docs.test:4173"] },
    screenshot: "off",
  },
  webServer: { command: "node scripts/serve.mjs out 4173", url: "http://127.0.0.1:4173/", reuseExistingServer: false, timeout: 20_000 },
  projects: [
    { name: "desktop", use: { viewport: { width: 1280, height: 800 } }, grepInvert: /@mobile/ },
    { name: "mobile", use: { viewport: { width: 375, height: 740 }, isMobile: true, hasTouch: true }, grep: /@mobile/ },
  ],
});
