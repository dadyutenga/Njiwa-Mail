import { expect, test } from "@playwright/test";
import { email, signIn, signedIn } from "./helpers";

test("a wrong password is refused and the page says so", async ({ page }) => {
  await signIn(page, "definitely-not-the-password");
  await expect(page.getByText("invalid_credentials")).toBeVisible();
  await expect(page).toHaveURL(/\/login/);
});

test("signing in opens the app, and the session survives a reload", async ({ page }) => {
  await signedIn(page);
  await page.reload();
  await expect(page).toHaveURL(/\/(app|p\/)/);
  const me = await page.request.get("/admin/v1/auth/me");
  expect((await me.json()).email).toBe(email);
});

test("a protected page redirects an anonymous visitor to the login", async ({ page }) => {
  await page.goto("/p/nothing/dashboard");
  await expect(page).toHaveURL(/\/login/);
});
