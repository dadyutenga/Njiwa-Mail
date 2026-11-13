import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, it, expect } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { AuthProvider } from "./auth";
import { AuthForm } from "./AuthForm";

describe("AuthForm", () => {
  it("renders login form and validates short password", async () => {
    render(
      <MemoryRouter>
        <QueryClientProvider client={new QueryClient()}>
          <AuthProvider>
            <AuthForm mode="login" />
          </AuthProvider>
        </QueryClientProvider>
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Welcome back");
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
    expect(screen.queryByLabelText(/Authenticator code/i)).not.toBeInTheDocument();

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
        <QueryClientProvider client={new QueryClient()}>
          <AuthProvider>
            <AuthForm mode="forgot-password" />
          </AuthProvider>
        </QueryClientProvider>
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Forgot password");
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Send Instructions" })).toBeInTheDocument();
  });
});

it("explains why the sign-in page is shown after a session ended", () => {
  sessionStorage.setItem("njiwa.signedOut", "1");
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <AuthProvider>
          <AuthForm mode="login" />
        </AuthProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(screen.getByRole("status")).toHaveTextContent("You were signed out.");
  expect(sessionStorage.getItem("njiwa.signedOut")).toBeNull();
});
