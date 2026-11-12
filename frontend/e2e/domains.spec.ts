import { expect, test } from "@playwright/test";
import { ensureProject, signedIn } from "./helpers";

test("adding a domain shows the exact DNS records to publish and a failing check", async ({
  page,
}) => {
  await signedIn(page);
  await ensureProject(page, "e2e");
  await page.goto("/p/e2e/domains");
  await page.getByRole("button", { name: "Add domain" }).first().click();
  await page.getByPlaceholder("notify.example.com").fill("e2e-notify.example.com");
  await page.getByRole("dialog").getByRole("button", { name: "Add domain" }).click();

  await expect(page.getByText("njiwa-mail-verify=").first()).toBeVisible();
  await expect(page.getByRole("heading", { name: /DNS records/i })).toBeVisible();
  // Ownership is not published, so the first record is not passing (announced as such).
  await expect(page.getByRole("img", { name: "Not passing" }).first()).toBeVisible();

  await page
    .getByRole("button", { name: /Check now/i })
    .first()
    .click();
  await expect(
    page.getByText(/not (found|published)|No (TXT|DKIM)|nothing/i).first(),
  ).toBeVisible();
});
