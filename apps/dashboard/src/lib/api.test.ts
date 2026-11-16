import { afterEach, describe, expect, it, vi } from "vitest";
import { addRequestInterceptor, addResponseInterceptor, apiRequest } from "./api";
afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
});
describe("API client", () => {
  it("applies interceptors and parses JSON", async () => {
    vi.stubEnv("VITE_API_BASE_URL", "https://example.test/");
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const removeRequest = addRequestInterceptor((request) => ({
      ...request,
      headers: { "X-Demo": "yes" },
    }));
    const responseSpy = vi.fn((response: Response) => response);
    const removeResponse = addResponseInterceptor(responseSpy);
    try {
      await expect(apiRequest("/example")).resolves.toEqual({ ok: true });
      expect(fetchMock).toHaveBeenCalledWith(
        new URL("https://example.test/example"),
        expect.objectContaining({ headers: { "X-Demo": "yes" } }),
      );
      expect(responseSpy).toHaveBeenCalledOnce();
    } finally {
      removeRequest();
      removeResponse();
    }
  });
  it("normalizes HTTP failures", async () => {
    vi.stubEnv("VITE_API_BASE_URL", "https://example.test/");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("", { status: 403, statusText: "Forbidden" })),
    );
    await expect(apiRequest("/example")).rejects.toMatchObject({
      status: 403,
      message: "Forbidden",
      name: "ApiError",
    });
  });
});
