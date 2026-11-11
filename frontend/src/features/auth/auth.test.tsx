import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { AuthProvider, ProtectedRoute } from "./auth";
describe("ProtectedRoute", () => {
  it("redirects anonymous visitors to login", () => {
    render(
      <MemoryRouter initialEntries={["/private"]}>
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
      </MemoryRouter>,
    );
    expect(screen.getByText("Sign in")).toBeInTheDocument();
  });
  it("denies a signed-in member access to an admin route", () => {
    localStorage.setItem("template-user", JSON.stringify({ name: "Demo", role: "member" }));
    render(
      <MemoryRouter initialEntries={["/private"]}>
        <AuthProvider>
          <Routes>
            <Route
              path="/private"
              element={
                <ProtectedRoute role="admin">
                  <div>Private</div>
                </ProtectedRoute>
              }
            />
            <Route path="/403" element={<div>Denied</div>} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>,
    );
    expect(screen.getByText("Denied")).toBeInTheDocument();
  });
});
