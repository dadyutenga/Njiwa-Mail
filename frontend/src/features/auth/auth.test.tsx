import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { admin } from "@/lib/admin-api";
import { AuthProvider, ProtectedRoute } from "./auth";
afterEach(() => vi.restoreAllMocks());
const user = {
  id: "u1",
  email: "admin@example.com",
  role: "admin" as const,
  csrf_token: "csrf",
  expires_at: "2030-01-01T00:00:00Z",
};
describe("ProtectedRoute", () => {
  it("allows admin users into operator screens", async () => {
    vi.spyOn(admin, "me").mockResolvedValue(user);
    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <QueryClientProvider client={new QueryClient()}>
          <AuthProvider>
            <Routes>
              <Route
                path="/dashboard"
                element={
                  <ProtectedRoute>
                    <div>Operator dashboard</div>
                  </ProtectedRoute>
                }
              />
            </Routes>
          </AuthProvider>
        </QueryClientProvider>
      </MemoryRouter>,
    );
    expect(await screen.findByText("Operator dashboard")).toBeInTheDocument();
  });
  it("redirects anonymous visitors after checking the server session", async () => {
    vi.spyOn(admin, "me").mockRejectedValue(new Error("unauthenticated"));
    render(
      <MemoryRouter initialEntries={["/private"]}>
        <QueryClientProvider client={new QueryClient()}>
          <AuthProvider>
            <Routes>
              <Route
                path="/private"
                element={
                  <ProtectedRoute>
                    <div>Private</div>
                  </ProtectedRoute>
                }
              />
              <Route path="/login" element={<div>Sign in</div>} />
            </Routes>
          </AuthProvider>
        </QueryClientProvider>
      </MemoryRouter>,
    );
    expect(await screen.findByText("Sign in")).toBeInTheDocument();
  });
  it("allows admin users to manage users", async () => {
    vi.spyOn(admin, "me").mockResolvedValue(user);
    render(
      <MemoryRouter initialEntries={["/users"]}>
        <QueryClientProvider client={new QueryClient()}>
          <AuthProvider>
            <Routes>
              <Route
                path="/users"
                element={
                  <ProtectedRoute>
                    <div>User management</div>
                  </ProtectedRoute>
                }
              />
            </Routes>
          </AuthProvider>
        </QueryClientProvider>
      </MemoryRouter>,
    );
    expect(await screen.findByText("User management")).toBeInTheDocument();
  });
});
