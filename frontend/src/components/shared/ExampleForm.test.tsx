import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ExampleForm } from "./ExampleForm";
describe("ExampleForm", () => {
  it("validates fields and submits valid values", async () => {
    const submit = vi.fn();
    render(<ExampleForm onSubmit={submit} />);
    fireEvent.click(screen.getByRole("button", { name: "Save example" }));
    expect(await screen.findByText("Enter at least two characters.")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Alex" } });
    fireEvent.change(screen.getByLabelText("Email"), { target: { value: "alex@example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Save example" }));
    await waitFor(() =>
      expect(submit).toHaveBeenCalledWith({ name: "Alex", email: "alex@example.com", notes: "" }),
    );
  });
});
