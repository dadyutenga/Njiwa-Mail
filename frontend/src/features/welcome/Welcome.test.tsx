import { expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { AuthProvider } from "@/features/auth/auth";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import Welcome from "./Welcome";

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/"]}>
      <AuthProvider>
        <Routes>
          <Route path="/" element={<Welcome />} />
          <Route path="/dashboard" element={<div>Dashboard destination</div>} />
        </Routes>
      </AuthProvider>
    </MemoryRouter>,
  );
}
it("renders the public welcome page and all feature cards", () => {
  renderPage();
  expect(screen.getByRole("heading", { name: "Your own mail carrier." })).toBeInTheDocument();
  expect(screen.getAllByRole("article")).toHaveLength(8);
  expect(screen.getByText("What is Njiwa-Mail?")).toBeInTheDocument();
});
it("redirects a signed-in visitor", () => {
  localStorage.setItem("template-user", JSON.stringify({ name: "Ada", role: "member" }));
  renderPage();
  expect(screen.getByText("Dashboard destination")).toBeInTheDocument();
});
it("switches theme and saves the choice", () => {
  document.documentElement.classList.remove("dark");
  renderPage();
  fireEvent.click(screen.getByRole("button", { name: "Light theme" }));
  expect(document.documentElement).toHaveClass("dark");
  expect(localStorage.getItem("theme")).toBe("dark");
  expect(screen.getByRole("button", { name: "Dark theme" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  document.documentElement.classList.remove("dark");
});
it("operates code tabs and FAQ by keyboard", () => {
  renderPage();
  fireEvent.keyDown(screen.getByRole("tab", { name: "cURL" }), { key: "ArrowRight" });
  expect(screen.getByRole("tab", { name: "Go" })).toHaveAttribute("aria-selected", "true");
  const question = screen.getByText("What is Njiwa-Mail?");
  fireEvent.click(question);
  expect(question.closest("details")).toHaveAttribute("open");
});
