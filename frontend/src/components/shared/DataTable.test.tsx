import { describe, it, expect } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { DataTable } from "./DataTable";
describe("DataTable", () => {
  it("filters and selects rows", () => {
    render(
      <DataTable
        rows={[
          { id: "1", name: "Alpha" },
          { id: "2", name: "Beta" },
        ]}
        columns={[{ key: "name", label: "Name" }]}
      />,
    );
    fireEvent.change(screen.getByLabelText("Search records"), { target: { value: "Alpha" } });
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.queryByText("Beta")).not.toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Select 1"));
    expect(screen.getByText("1 selected")).toBeInTheDocument();
  });
  it("sorts and pages records", () => {
    const rows = Array.from({ length: 6 }, (_, index) => ({
      id: String(index),
      name: `Item ${index}`,
    }));
    render(<DataTable rows={rows} columns={[{ key: "name", label: "Name" }]} />);
    expect(screen.queryByText("Item 5")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getByText("Item 5")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Name/ }));
    expect(screen.getByText("Item 0")).toBeInTheDocument();
  });
});
