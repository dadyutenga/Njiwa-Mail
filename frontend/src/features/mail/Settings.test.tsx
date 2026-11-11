import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "@/features/workspace/context";
import Settings from "./Settings";

afterEach(() => vi.restoreAllMocks());

it("shares settings and administration links within the current workspace", async () => {
  vi.spyOn(admin, "get").mockImplementation(async (path) =>
    path === "/settings" ? { login_attempts_per_account: 20 } : {},
  );
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <WorkspaceProvider project="demo">
          <Settings />
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByRole("button", { name: "Save settings" })).toBeEnabled();
  expect(
    screen.getByText("Every enabled user is an admin and can access all pages and settings."),
  ).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Users" })).toHaveAttribute("href", "/p/demo/users");
  expect(screen.getByRole("link", { name: "Mail server" })).toHaveAttribute(
    "href",
    "/p/demo/mail-server",
  );
  expect(screen.getByRole("link", { name: "Recipient privacy" })).toHaveAttribute(
    "href",
    "/p/demo/privacy",
  );
});
