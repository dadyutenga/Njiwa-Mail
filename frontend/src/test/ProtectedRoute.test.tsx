import { render, screen } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";
import { ProtectedRoute } from "@/components/ProtectedRoute";
import * as AuthContextModule from "@/contexts/AuthContext";
import type { User } from "@/types/auth";

describe("ProtectedRoute Component", () => {
  it("renders protected content when user is authenticated", () => {
    const mockUser: User = {
      id: "usr_1",
      name: "Test User",
      email: "test@example.com",
      role: "admin",
      createdAt: new Date().toISOString(),
    };

    vi.spyOn(AuthContextModule, "useAuth").mockReturnValue({
      user: mockUser,
      token: "valid-token",
      isAuthenticated: true,
      isLoading: false,
      login: vi.fn(),
      register: vi.fn(),
      logout: vi.fn(),
      updateUser: vi.fn(),
      switchRole: vi.fn(),
    });

    render(
      <MemoryRouter initialEntries={["/protected"]}>
        <Routes>
          <Route
            path="/protected"
            element={
              <ProtectedRoute>
                <div>Secret Dashboard Area</div>
              </ProtectedRoute>
            }
          />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByText("Secret Dashboard Area")).toBeInTheDocument();
  });

  it("redirects to login when unauthenticated", () => {
    vi.spyOn(AuthContextModule, "useAuth").mockReturnValue({
      user: null,
      token: null,
      isAuthenticated: false,
      isLoading: false,
      login: vi.fn(),
      register: vi.fn(),
      logout: vi.fn(),
      updateUser: vi.fn(),
      switchRole: vi.fn(),
    });

    render(
      <MemoryRouter initialEntries={["/protected"]}>
        <Routes>
          <Route
            path="/protected"
            element={
              <ProtectedRoute>
                <div>Secret Dashboard Area</div>
              </ProtectedRoute>
            }
          />
          <Route path="/login" element={<div>Login Page Redirected</div>} />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.queryByText("Secret Dashboard Area")).not.toBeInTheDocument();
    expect(screen.getByText("Login Page Redirected")).toBeInTheDocument();
  });
});
