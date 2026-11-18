import { describe, expect, it } from "vitest";
import { parseFieldLines, variableValue } from "./variables";

describe("template variables", () => {
  it("turns Label: value lines into details rows", () => {
    expect(parseFieldLines("Order: #1042\n\n Amount : TZS 25,000 \nTime: 10:30\nGift")).toEqual([
      { label: "Order", value: "#1042" },
      { label: "Amount", value: "TZS 25,000" },
      { label: "Time", value: "10:30" },
      { label: "Gift", value: "" },
    ]);
  });
  it("converts by type", () => {
    expect(variableValue("int", "5")).toBe(5);
    expect(variableValue("string", "5")).toBe("5");
    expect(variableValue("fields", "A: 1")).toEqual([{ label: "A", value: "1" }]);
  });
});
