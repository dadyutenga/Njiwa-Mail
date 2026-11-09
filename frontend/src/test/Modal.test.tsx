import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { Modal } from "@/components/shared/Modal";

describe("Modal Component", () => {
  it("renders title, description and children when open is true", () => {
    render(
      <Modal
        open={true}
        onOpenChange={() => {}}
        title="Test Modal Title"
        description="This is a modal description"
      >
        <p>Modal body content</p>
      </Modal>,
    );

    expect(screen.getByText("Test Modal Title")).toBeInTheDocument();
    expect(screen.getByText("This is a modal description")).toBeInTheDocument();
    expect(screen.getByText("Modal body content")).toBeInTheDocument();
  });

  it("does not render contents when open is false", () => {
    render(
      <Modal
        open={false}
        onOpenChange={() => {}}
        title="Hidden Modal"
      >
        <p>Hidden body</p>
      </Modal>,
    );

    expect(screen.queryByText("Hidden Modal")).not.toBeInTheDocument();
    expect(screen.queryByText("Hidden body")).not.toBeInTheDocument();
  });
});
