import { afterEach, expect, it, vi } from "vitest";
import { admin, setCsrf } from "./admin-api";
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  setCsrf("");
});
it("sends the session cookie and CSRF token on writes", async () => {
  setCsrf("csrf-example");
  const fetchMock = vi
    .fn()
    .mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));
  vi.stubGlobal("fetch", fetchMock);
  await admin.send("/projects", "POST", { name: "test" });
  const [url, options] = fetchMock.mock.calls[0] as [string, RequestInit];
  expect(url).toBe("/admin/v1/projects");
  expect(options.credentials).toBe("same-origin");
  expect(new Headers(options.headers).get("X-CSRF-Token")).toBe("csrf-example");
});
it("reports backend permission errors without hiding them", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "forbidden", message: "Access denied" } }), {
        status: 403,
      }),
    ),
  );
  await expect(admin.get("/users")).rejects.toMatchObject({
    status: 403,
    code: "forbidden",
    message: "Access denied",
  });
});
