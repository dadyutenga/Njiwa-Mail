import { describe, it, expect } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { AuthProvider } from "./auth";
import { AuthForm } from "./AuthForm";

describe("AuthForm", () => {
  it("renders login form and validates short password", async () => {
    render(
      <MemoryRouter>
        <AuthProvider>
          <AuthForm mode="login" />
        </AuthProvider>
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Welcome back");
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "test@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "123" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => {
      expect(screen.getByText("Password must be at least 8 characters")).toBeInTheDocument();
    });
  });

  it("renders forgot-password form without password field", () => {
    render(
      <MemoryRouter>
        <AuthProvider>
          <AuthForm mode="forgot-password" />
        </AuthProvider>
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Forgot password");
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Send Instructions" })).toBeInTheDocument();
  });
});
