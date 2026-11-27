import { expect, test } from "@playwright/test";
import { email, password } from "./helpers";

// Runs first, on a fresh stack: the whole first-run wizard in a real browser.
// It creates the account the other specs sign in with.
test("first-run setup creates the account once and then closes", async ({ page }) => {
  const status = await (await page.request.get("/admin/v1/setup/status")).json();
  test.skip(!status.needs_setup, "this installation is already set up");

  await page.goto("/login"); // any page leads to the setup
  await expect(page).toHaveURL(/\/setup/);
  await page.getByRole("button", { name: "Start" }).click();
  await page.getByLabel("Organization name").fill("CI Organization");
  await page.getByRole("button", { name: "Next" }).click();
  await page.getByLabel("Your name").fill("CI Operator");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Next" }).click();
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page.getByText("Your account is ready")).toBeVisible();
  const me = await page.request.get("/admin/v1/auth/me");
  expect((await me.json()).email).toBe(email);

  const after = await (await page.request.get("/admin/v1/setup/status")).json();
  expect(after.needs_setup).toBe(false);
  const again = await page.request.post("/admin/v1/setup", {
    data: { organization: "Other", name: "Other", email: "other@example.com", password },
    headers: { "X-Njiwa-Setup": "1" },
  });
  expect(again.status()).toBe(409);
  await page.context().clearCookies();
  await page.goto("/setup");
  await expect(page).toHaveURL(/\/login/);
});
