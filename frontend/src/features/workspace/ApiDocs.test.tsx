import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "./context";

const links = vi.hoisted(() => ({ docs: "", github: "" }));
vi.mock("@/features/welcome/config", () => ({ welcomeLinks: links }));

import { ApiDocs } from "./pages/Developers";

beforeEach(() => {
  vi.spyOn(admin, "get").mockResolvedValue({ operations: [] } as never);
});
afterEach(() => vi.restoreAllMocks());

function renderDocs() {
  render(
    <MemoryRouter>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <WorkspaceProvider project="demo">
          <ApiDocs />
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

it("links to the public documentation when the operator configured it, opening safely", async () => {
  links.docs = "https://docs.example.com/";
  renderDocs();
  const link = await screen.findByRole("link", { name: "Open the documentation" });
  expect(link).toHaveAttribute("href", "https://docs.example.com/docs");
  expect(link).toHaveAttribute("rel", "noopener noreferrer");
  expect(link).toHaveAttribute("target", "_blank");
});

it("shows how to get a documentation link instead of a dead one when none is configured", async () => {
  links.docs = "";
  renderDocs();
  expect(await screen.findByText(/No documentation site is configured/)).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Open the documentation" })).not.toBeInTheDocument();
});
