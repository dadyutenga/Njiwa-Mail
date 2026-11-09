import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { DataTable, type ColumnDef } from "@/components/shared/DataTable";

interface TestItem {
  id: string;
  name: string;
  category: string;
  count: number;
}

const mockData: TestItem[] = [
  { id: "1", name: "Alpha Item", category: "Hardware", count: 10 },
  { id: "2", name: "Beta Item", category: "Software", count: 25 },
  { id: "3", name: "Gamma Item", category: "Hardware", count: 5 },
];

const mockColumns: ColumnDef<TestItem>[] = [
  { key: "name", header: "Name", sortable: true },
  { key: "category", header: "Category", sortable: true },
  { key: "count", header: "Count", sortable: true },
];

describe("DataTable Component", () => {
  it("renders table headers and data rows correctly", () => {
    render(<DataTable data={mockData} columns={mockColumns} />);

    expect(screen.getByText("Name")).toBeInTheDocument();
    expect(screen.getByText("Category")).toBeInTheDocument();
    expect(screen.getByText("Count")).toBeInTheDocument();

    expect(screen.getByText("Alpha Item")).toBeInTheDocument();
    expect(screen.getByText("Beta Item")).toBeInTheDocument();
    expect(screen.getByText("Gamma Item")).toBeInTheDocument();
  });

  it("filters items based on search input", () => {
    render(<DataTable data={mockData} columns={mockColumns} searchPlaceholder="Search test..." />);

    const searchInput = screen.getByPlaceholderText("Search test...");
    fireEvent.change(searchInput, { target: { value: "Beta" } });

    expect(screen.getByText("Beta Item")).toBeInTheDocument();
    expect(screen.queryByText("Alpha Item")).not.toBeInTheDocument();
  });

  it("displays empty state when no items match search", () => {
    render(<DataTable data={mockData} columns={mockColumns} searchPlaceholder="Search test..." />);

    const searchInput = screen.getByPlaceholderText("Search test...");
    fireEvent.change(searchInput, { target: { value: "Nonexistent" } });

    expect(screen.getByText("No records found")).toBeInTheDocument();
  });

  it("toggles select all rows", () => {
    render(<DataTable data={mockData} columns={mockColumns} />);

    const selectAllCheckbox = screen.getByLabelText("Select all");
    fireEvent.click(selectAllCheckbox);

    expect(screen.getByText("3 rows selected")).toBeInTheDocument();
  });
});
