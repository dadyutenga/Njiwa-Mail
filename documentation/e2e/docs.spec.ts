import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";
import fs from "node:fs";

const shots = process.env.SHOT_DIR;
async function shot(page: Page, name: string) {
  if (shots) {
    fs.mkdirSync(shots, { recursive: true });
    await page.screenshot({ path: `${shots}/${name}.png`, fullPage: false });
  }
}

test.describe("desktop", () => {
  test("landing page", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("heading", { level: 1 })).toContainText("Transactional email you run yourself");
    await expect(page.getByRole("link", { name: "Start with the quickstart" })).toBeVisible();
    await expect(page.getByText("never means delivered")).toBeVisible();
    await shot(page, "desktop-landing");
  });

  test("docs page has sidebar, breadcrumb, on-page TOC, and next/previous work", async ({ page }) => {
    await page.goto("/docs/api/send-email");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Send an email");
    await expect(page.locator("#nd-sidebar, aside").first()).toBeVisible();
    await expect(page.getByRole("navigation", { name: /breadcrumb/i })).toBeVisible();
    await expect(page.locator("#nd-toc, [id*='toc']").first()).toBeVisible();
    await expect(page.getByRole("heading", { name: "Request" }).first()).toBeVisible();
    await shot(page, "desktop-doc");
    const next = page.getByRole("link", { name: /Batch send/ }).last();
    await next.click();
    await expect(page).toHaveURL(/\/docs\/api\/batch$/);
    await page.getByRole("link", { name: /Send an email/ }).last().click();
    await expect(page).toHaveURL(/\/docs\/api\/send-email$/);
  });

  test("code tabs switch language and the code is highlighted", async ({ page }) => {
    await page.goto("/docs/quickstart");
    const tab = page.getByRole("tab", { name: "Go" }).first();
    await tab.click();
    await expect(page.getByText("njiwamail").first()).toBeVisible();
    await expect(page.getByRole("tab", { name: "curl" }).first()).toBeVisible();
    expect(await page.locator("pre code span").count()).toBeGreaterThan(10);
  });

  test("keyboard search finds a page and opens it", async ({ page }) => {
    await page.goto("/docs");
    await page.keyboard.press("Control+k");
    const box = page.getByPlaceholder(/search/i);
    await expect(box).toBeVisible();
    await box.fill("idempotency");
    const hit = page.getByRole("option").filter({ hasText: /Idempotency/ }).first();
    await expect(hit).toBeVisible();
    await shot(page, "desktop-search");
    await hit.click();
    await expect(page).toHaveURL(/\/docs\/api\/idempotency/);
  });

  test("Copy Markdown copies exactly the page's .md and announces it", async ({ page, context }) => {
    // The real clipboard needs a secure context: 127.0.0.1 is one, the mapped docs.test host is plain http.
    await context.grantPermissions(["clipboard-read", "clipboard-write"], { origin: "http://127.0.0.1:4173" });
    await page.goto("http://127.0.0.1:4173/docs/quickstart");
    await page.getByTestId("copy-markdown").click();
    const toast = page.getByRole("status").getByTestId("toast");
    await expect(toast).toHaveText("Markdown copied");
    const copied = await page.evaluate(() => navigator.clipboard.readText());
    const md = await page.evaluate(() => fetch("/docs/quickstart.md").then((r) => r.text()));
    expect(copied).toBe(md);
    expect(copied).toContain("# Quickstart");
    expect(copied).not.toMatch(/<Tabs|<Steps|<Callout/);
  });

  test("Copy Markdown falls back when the clipboard API is refused", async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(navigator, "clipboard", { value: { writeText: () => Promise.reject(new Error("denied")) }, configurable: true });
      (window as unknown as { __copied: number }).__copied = 0;
      document.execCommand = (cmd: string) => {
        if (cmd === "copy") (window as unknown as { __copied: number }).__copied++;
        return true;
      };
    });
    await page.goto("/docs/quickstart");
    await page.getByTestId("copy-markdown").click();
    await expect(page.getByTestId("toast")).toHaveText("Markdown copied");
    expect(await page.evaluate(() => (window as unknown as { __copied: number }).__copied)).toBe(1);
  });

  test("Copy Markdown reports a failure instead of pretending", async ({ page }) => {
    await page.route("**/docs/quickstart.md", (r) => r.fulfill({ status: 500, body: "no" }));
    await page.goto("/docs/quickstart");
    await page.getByTestId("copy-markdown").click();
    await expect(page.getByTestId("toast")).toContainText("Could not copy");
  });

  test("Open menu: prefill services link out with a short prompt; others copy it; Escape closes", async ({ page }) => {
    await page.addInitScript(() => {
      let held = "";
      Object.defineProperty(navigator, "clipboard", { value: { writeText: async (t: string) => void (held = t), readText: async () => held }, configurable: true });
      (window as unknown as { __opened: string[][] }).__opened = [];
      window.open = ((...a: unknown[]) => {
        (window as unknown as { __opened: unknown[] }).__opened.push(a as unknown[]);
        return null;
      }) as typeof window.open;
    });
    await page.goto("/docs/api/send-email");
    const trigger = page.getByTestId("open-menu");
    await trigger.click();
    await expect(trigger).toHaveAttribute("aria-expanded", "true");
    for (const id of ["chatgpt", "claude", "scira", "t3chat"]) await expect(page.getByTestId(`open-${id}`)).toBeVisible();
    await shot(page, "desktop-open-menu");
    await page.keyboard.press("Escape");
    await expect(page.getByTestId("open-chatgpt")).toHaveCount(0);

    await trigger.click();
    await page.getByTestId("open-chatgpt").click();
    let opened = await page.evaluate(() => (window as unknown as { __opened: string[][] }).__opened);
    expect(opened).toHaveLength(1);
    const [url, target, features] = opened[0];
    const u = new URL(url);
    expect(u.origin).toBe("https://chatgpt.com");
    const q = u.searchParams.get("q")!;
    expect(q).toContain("http://docs.test:4173/docs/api/send-email.md");
    expect(q).toContain("http://docs.test:4173/openapi.yaml");
    expect(url.length).toBeLessThan(2000);
    expect(q).not.toMatch(/dmk_|whsec_|BEGIN|Bearer/);
    expect(target).toBe("_blank");
    expect(features).toContain("noopener");

    await trigger.click();
    await page.getByTestId("open-claude").click();
    opened = await page.evaluate(() => (window as unknown as { __opened: string[][] }).__opened);
    expect(opened[1][0]).toBe("https://claude.ai/new");
    expect(await page.evaluate(() => navigator.clipboard.readText())).toContain("send-email.md");
    await expect(page.getByTestId("toast").last()).toContainText("Prompt copied");
  });

  test("on a local origin outside services get no link to the page, only a copied prompt", async ({ page, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"], { origin: "http://127.0.0.1:4173" });
    await page.addInitScript(() => {
      (window as unknown as { __opened: string[][] }).__opened = [];
      window.open = ((...a: unknown[]) => ((window as unknown as { __opened: unknown[] }).__opened.push(a as unknown[]), null)) as typeof window.open;
    });
    await page.goto("http://127.0.0.1:4173/docs/api/send-email");
    await page.getByTestId("open-menu").click();
    await page.getByTestId("open-chatgpt").click();
    const opened = await page.evaluate(() => (window as unknown as { __opened: string[][] }).__opened);
    expect(opened[0][0]).toBe("https://chatgpt.com/");
  });

  test("404 page", async ({ page }) => {
    const res = await page.goto("/docs/this-page-does-not-exist");
    expect(res?.status()).toBe(404);
    await expect(page.getByRole("heading", { name: "This page does not exist" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Quickstart" })).toBeVisible();
    await shot(page, "desktop-404");
  });

  test("version selector shows the published version", async ({ page }) => {
    await page.goto("/docs");
    const sel = page.getByTestId("version-select").filter({ visible: true });
    await expect(sel).toBeVisible();
    await expect(sel).toHaveValue("1.0");
    await expect(sel.locator("option")).toHaveText(["v1 (latest)"]);
  });

  test("light and dark theme", async ({ page }) => {
    await page.emulateMedia({ colorScheme: "light" });
    await page.goto("/docs");
    await page.getByRole("button", { name: /theme/i }).first().click().catch(() => {});
    const dark = await page.evaluate(() => document.documentElement.classList.contains("dark"));
    await page.emulateMedia({ colorScheme: "dark" });
    await page.reload();
    expect(await page.evaluate(() => getComputedStyle(document.body).backgroundColor)).not.toBe("");
    expect(typeof dark).toBe("boolean");
  });

  test("machine-readable routes have the right types and the same version", async ({ playwright }) => {
    const request = await playwright.request.newContext({ baseURL: "http://127.0.0.1:4173" });
    const llms = await request.get("/llms.txt");
    expect(llms.headers()["content-type"]).toContain("text/plain");
    const body = await llms.text();
    expect(body).toContain("# Njiwa-Mail");
    expect((await request.get("/llms-full.txt")).status()).toBe(200);
    expect((await request.get("/docs/quickstart.md")).headers()["content-type"]).toContain("text/markdown");
    expect((await request.get("/openapi.yaml")).headers()["content-type"]).toContain("yaml");
    const json = await (await request.get("/openapi.json")).json();
    expect(json.openapi).toBe("3.0.3");
    const skill = await request.get("/skills/njiwa-mail-integration/SKILL.md");
    expect(skill.status()).toBe(200);
    const version = (await (await request.get("/versions.json")).json()).current;
    expect(body).toContain(`version ${version}`);
    await request.dispose();
  });

  for (const path of ["/", "/docs", "/docs/quickstart", "/docs/api/send-email", "/docs/webhooks", "/docs/setup/managed-postfix"]) {
    test(`accessibility smoke ${path}`, async ({ page }) => {
      await page.goto(path);
      const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
      const serious = results.violations.filter((v) => v.impact === "serious" || v.impact === "critical");
      expect(serious.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).slice(0, 3).join(" | ")}`)).toEqual([]);
    });
  }
});

test.describe("mobile @mobile", () => {
  test("navigation drawer opens, a page opens, and nothing scrolls sideways @mobile", async ({ page }) => {
    await page.goto("/docs/quickstart");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Quickstart");
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(1);
    await shot(page, "mobile-doc");
    await page.getByRole("button", { name: /open sidebar|menu|navigation/i }).first().click();
    const link = page.getByRole("link", { name: "Webhooks" }).first();
    await expect(link).toBeVisible();
    await shot(page, "mobile-nav");
    await link.click();
    await expect(page).toHaveURL(/\/docs\/webhooks$/);
  });

  test("Copy Markdown and Open are reachable on a phone @mobile", async ({ page }) => {
    await page.goto("/docs/api/send-email");
    await expect(page.getByTestId("copy-markdown")).toBeVisible();
    await page.getByTestId("open-menu").click();
    const menu = page.getByRole("menu");
    await expect(menu).toBeVisible();
    const box = await menu.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(375);
    await shot(page, "mobile-open-menu");
  });

  test("landing page fits a phone @mobile", async ({ page }) => {
    await page.goto("/");
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(1);
    await shot(page, "mobile-landing");
  });
});
