import { describe, it, expect } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import Records from "./Records";
import { MemoryRouter } from "react-router-dom";
describe("Records form", () => {
  it("creates a record in the dialog", () => {
    render(
      <MemoryRouter>
        <Records />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Create record" }));
    fireEvent.change(within(screen.getByRole("dialog")).getByLabelText("Name"), {
      target: { value: "Delta" },
    });
    fireEvent.change(within(screen.getByRole("dialog")).getByLabelText("Owner"), {
      target: { value: "Morgan" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(screen.getByText("Delta")).toBeInTheDocument();
  });
});
