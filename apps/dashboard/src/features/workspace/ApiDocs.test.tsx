import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "./context";

const links = vi.hoisted(() => ({ docs: "", github: "" }));
vi.mock("@/features/welcome/config", async (orig) => {
  const real = await orig<typeof import("@/features/welcome/config")>();
  return { ...real, welcomeLinks: links, docsHome: (o: string = links.docs) => real.docsHome(o) };
});

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
  const link = await screen.findByRole("link", { name: "API reference" });
  expect(link).toHaveAttribute("href", "https://docs.example.com/docs/api-reference");
  expect(link).toHaveAttribute("rel", "noopener noreferrer");
  expect(link).toHaveAttribute("target", "_blank");
});

it("shows how to get a documentation link instead of a dead one when none is configured", async () => {
  links.docs = "";
  renderDocs();
  expect(await screen.findByText(/No documentation site is configured/)).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "API reference" })).not.toBeInTheDocument();
});

it("maps each operation to the reference page the docs generator writes", async () => {
  const { referencePages } = await vi.importActual<typeof import("@/features/welcome/config")>(
    "@/features/welcome/config",
  );
  const pages = referencePages(
    {
      paths: {
        "/v1/emails": { post: { tags: ["Emails"], operationId: "sendEmail" } },
        "/v1/t/o/{token}": { get: { tags: ["Web view"], operationId: "trackOpen" } },
      },
    },
    "https://mail.example.com/docs",
  );
  // Same slugs as apps/docs/content/docs/api-reference (generate-reference.mjs).
  expect(pages.get("POST /v1/emails")).toBe(
    "https://mail.example.com/docs/api-reference/emails/send-email",
  );
  expect(pages.get("GET /v1/t/o/{token}")).toBe(
    "https://mail.example.com/docs/api-reference/web-view/track-open",
  );
});
