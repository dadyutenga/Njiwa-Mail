import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { CrudForm } from "@/features/example-crud/CrudForm";

describe("CrudForm Component", () => {
  it("renders form fields with default values", () => {
    render(<CrudForm onSubmit={() => {}} />);

    expect(screen.getByLabelText("Title")).toBeInTheDocument();
    expect(screen.getByLabelText("Unique Code")).toBeInTheDocument();
    expect(screen.getByLabelText("Assignee")).toBeInTheDocument();
    expect(screen.getByLabelText("Amount ($)")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create Record" })).toBeInTheDocument();
  });

  it("populates initial data when editing an existing record", () => {
    const initial = {
      id: "99",
      title: "Existing Project",
      code: "REC-9999",
      category: "Finance",
      status: "active" as const,
      priority: "high" as const,
      assignedTo: "Jane Doe",
      amount: 4200,
      description: "Existing test description",
      tags: ["finance"],
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };

    render(<CrudForm initialData={initial} onSubmit={() => {}} />);

    expect(screen.getByDisplayValue("Existing Project")).toBeInTheDocument();
    expect(screen.getByDisplayValue("REC-9999")).toBeInTheDocument();
    expect(screen.getByDisplayValue("Jane Doe")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Update Record" })).toBeInTheDocument();
  });

  it("submits valid form data", async () => {
    const handleSubmit = vi.fn();
    render(<CrudForm onSubmit={handleSubmit} />);

    const titleInput = screen.getByLabelText("Title");
    fireEvent.change(titleInput, { target: { value: "Brand New Feature" } });

    const submitBtn = screen.getByRole("button", { name: "Create Record" });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(handleSubmit).toHaveBeenCalledTimes(1);
      expect(handleSubmit).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "Brand New Feature",
        }),
        expect.anything(),
      );
    });
  });
});
