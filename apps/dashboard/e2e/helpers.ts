import { expect, type Page } from "@playwright/test";

export const email = process.env.E2E_EMAIL ?? "ci@example.com";
export const password = process.env.E2E_PASSWORD ?? "ci-password-12345";

/** Signs in through the real login form and waits for the app. */
export async function signIn(page: Page, pw = password) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(pw);
  await page.getByRole("button", { name: "Sign in" }).click();
}

export async function signedIn(page: Page) {
  await signIn(page);
  await expect(page).toHaveURL(/\/(app|p\/)/);
}

/** The CSRF token of the current session, for API calls that set up test data. */
export async function csrf(page: Page): Promise<string> {
  const res = await page.request.get("/admin/v1/auth/me");
  expect(res.ok()).toBeTruthy();
  return (await res.json()).csrf_token as string;
}

/** Creates a project through the API (idempotent for the test run). */
export async function ensureProject(page: Page, name: string) {
  const token = await csrf(page);
  const res = await page.request.post("/admin/v1/projects", {
    data: { name },
    headers: { "X-CSRF-Token": token },
  });
  expect([200, 201, 409]).toContain(res.status()); // 409: it already exists
}
