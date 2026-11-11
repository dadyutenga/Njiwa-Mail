import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { fireEvent, render, screen } from "@testing-library/react";
import { AuthProvider } from "@/features/auth/auth";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import Welcome from "./Welcome";

afterEach(() => vi.restoreAllMocks());
const user = {
  id: "u1",
  email: "ada@example.com",
  role: "admin" as const,
  csrf_token: "csrf",
  expires_at: "2030-01-01T00:00:00Z",
};
function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/"]}>
      <QueryClientProvider client={new QueryClient()}>
        <AuthProvider>
          <Routes>
            <Route path="/" element={<Welcome />} />
            <Route path="/dashboard" element={<div>Dashboard destination</div>} />
          </Routes>
        </AuthProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}
it("renders the public welcome page and all feature cards", async () => {
  vi.spyOn(admin, "me").mockRejectedValue(new Error("unauthenticated"));
  renderPage();
  expect(
    await screen.findByRole("heading", { name: "Your own mail carrier." }),
  ).toBeInTheDocument();
  expect(screen.getAllByRole("article")).toHaveLength(8);
  expect(screen.getByText("What is Njiwa-Mail?")).toBeInTheDocument();
});
it("redirects a signed-in visitor", async () => {
  vi.spyOn(admin, "me").mockResolvedValue(user);
  renderPage();
  expect(await screen.findByText("Dashboard destination")).toBeInTheDocument();
});
it("switches theme and saves the choice", async () => {
  document.documentElement.classList.remove("dark");
  vi.spyOn(admin, "me").mockRejectedValue(new Error("unauthenticated"));
  renderPage();
  fireEvent.click(await screen.findByRole("button", { name: "Light theme" }));
  expect(document.documentElement).toHaveClass("dark");
  expect(localStorage.getItem("theme")).toBe("dark");
  expect(screen.getByRole("button", { name: "Dark theme" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  document.documentElement.classList.remove("dark");
});
it("operates code tabs and FAQ by keyboard", async () => {
  vi.spyOn(admin, "me").mockRejectedValue(new Error("unauthenticated"));
  renderPage();
  fireEvent.keyDown(await screen.findByRole("tab", { name: "cURL" }), { key: "ArrowRight" });
  expect(screen.getByRole("tab", { name: "Go" })).toHaveAttribute("aria-selected", "true");
  const question = screen.getByText("What is Njiwa-Mail?");
  fireEvent.click(question);
  expect(question.closest("details")).toHaveAttribute("open");
});
