import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import Reports from "./Reports";
vi.mock("@/components/shared/Charts", () => ({
  BarChartView: ({ data }: { data: { name: string }[] }) => (
    <div>{data.map((row) => row.name).join(",")}</div>
  ),
  LineChartView: () => null,
  PieChartView: () => null,
}));
describe("Reports", () => {
  it("filters mock data by date and detects invalid ranges", () => {
    render(<Reports />);
    expect(screen.getByText("Jan,Feb,Mar,Apr,May,Jun")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-04-01" } });
    expect(screen.getByText("Apr,May,Jun")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2026-03-01" } });
    expect(screen.getByRole("alert")).toHaveTextContent("start date");
    expect(screen.getByRole("button", { name: "Export CSV" })).toBeDisabled();
  });
});
