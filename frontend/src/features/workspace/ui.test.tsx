import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { Field } from "./ui";

it("labels the control, links its hint, and marks it invalid with a described error", () => {
  const { rerender } = render(
    <Field label="Email" hint="We never share it">
      <input />
    </Field>,
  );
  const input = screen.getByLabelText("Email");
  expect(input).not.toHaveAttribute("aria-invalid");
  expect(input).toHaveAccessibleDescription("We never share it");
  // The hint is a description, not part of the accessible name.
  expect(screen.getByRole("textbox", { name: "Email" })).toBeInTheDocument();

  rerender(
    <Field label="Email" hint="We never share it" error="Enter an email address.">
      <input />
    </Field>,
  );
  const bad = screen.getByLabelText("Email");
  expect(bad).toHaveAttribute("aria-invalid", "true");
  expect(bad).toHaveAccessibleDescription("Enter an email address.");
  expect(screen.getByRole("alert")).toHaveTextContent("Enter an email address.");
});

it("keeps a wrapping label for composite children", () => {
  render(
    <Field label="Pick one">
      <div>
        <input aria-label="a" />
      </div>
    </Field>,
  );
  expect(screen.getByLabelText("a")).toBeInTheDocument();
});
