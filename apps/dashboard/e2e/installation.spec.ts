import { expect, test } from "@playwright/test";
import { csrf, ensureProject, signedIn } from "./helpers";

test("the delivery-mode wizard saves the choice only after Continue, and it survives a reload", async ({
  page,
}) => {
  await signedIn(page);
  await ensureProject(page, "e2e");
  await page.goto("/p/e2e/mail-server");
  await expect(page.getByText("How will you send email?")).toBeVisible();

  const cont = page.getByRole("button", { name: "Continue", exact: true });
  await expect(cont).toBeDisabled(); // nothing chosen yet
  await page.getByLabel(/Connect an existing mail server/).check();
  // Choosing alone persists nothing.
  let state = await (await page.request.get("/admin/v1/installation")).json();
  expect(JSON.stringify(state.wizard ?? {})).not.toContain("existing_smtp");

  await cont.click();
  await expect
    .poll(async () =>
      JSON.stringify(
        (await (await page.request.get("/admin/v1/installation")).json()).wizard ?? {},
      ),
    )
    .toContain("existing_smtp");

  await page.reload();
  state = await (await page.request.get("/admin/v1/installation")).json();
  expect(state.wizard.selected_mode).toBe("existing_smtp");
  await expect(
    page.getByRole("heading", { name: /Delivery infrastructure/i }).first(),
  ).toBeVisible();
  // The API refuses a write without the CSRF token (the page always sends it).
  const noCsrf = await page.request.put("/admin/v1/installation/wizard", {
    data: { selected_mode: "managed_mta" },
  });
  expect(noCsrf.status()).toBe(403);
  expect(await csrf(page)).toBeTruthy();
});
