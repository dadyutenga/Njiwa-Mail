import { describe, expect, it } from "vitest";
import { safeRedirect } from "./redirect";

describe("safeRedirect", () => {
  it("keeps an in-app path", () => {
    expect(safeRedirect("/messages/abc")).toBe("/messages/abc");
  });

  it("rejects absolute and protocol-relative targets", () => {
    expect(safeRedirect("https://evil.example/phish")).toBe("/dashboard");
    expect(safeRedirect("//evil.example")).toBe("/dashboard");
    expect(safeRedirect("/\\evil.example")).toBe("/dashboard");
    expect(safeRedirect(null)).toBe("/dashboard");
  });
});
