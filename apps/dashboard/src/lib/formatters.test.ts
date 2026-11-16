import { describe, it, expect } from "vitest";
import { formatBytes, formatCurrency, formatDate, formatNumber } from "./formatters";

describe("formatters", () => {
  it("formats numbers with locale separators", () => {
    expect(formatNumber(1250000, "en-US")).toBe("1,250,000");
  });

  it("formats currency accurately", () => {
    expect(formatCurrency(49.99, "USD", "en-US")).toBe("$49.99");
  });

  it("formats date in medium dateStyle", () => {
    const formatted = formatDate("2026-04-15T00:00:00Z", "en-US");
    expect(formatted).toContain("2026");
    expect(formatted).toContain("Apr");
  });

  it("formats bytes into human readable units", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1024)).toBe("1 KB");
    expect(formatBytes(1048576)).toBe("1 MB");
    expect(formatBytes(1073741824)).toBe("1 GB");
    expect(formatBytes(-1)).toBe("—");
  });
});
