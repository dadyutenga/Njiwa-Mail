import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "@/features/workspace/context";
import { navGroups, findNav } from "../nav";
import { Tabbed } from "./CampaignTabs";
import { NewsletterEditor } from "./Newsletters";

afterEach(() => {
  vi.restoreAllMocks();
});

function wrap(ui: ReactNode, path: string, route: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter initialEntries={[path]}>
      <QueryClientProvider client={client}>
        <WorkspaceProvider project="demo">
          <Routes>
            <Route path={route} element={ui} />
          </Routes>
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

it("has one Campaigns & Audience entry whose tabs link to each other", () => {
  const labels = navGroups.flatMap((g) => g.items.map((i) => i.label));
  expect(labels).toContain("Campaigns & Audience");
  expect(labels).not.toContain("Bulk send");
  for (const page of ["contacts", "bulk", "scheduled", "newsletters"])
    expect(findNav(page)?.item.label).toBe("Campaigns & Audience");
  wrap(
    <Tabbed>
      <p>page</p>
    </Tabbed>,
    "/p/demo/bulk",
    "/p/demo/bulk",
  );
  for (const [name, href] of [
    ["Audience", "/p/demo/contacts"],
    ["Campaigns", "/p/demo/bulk"],
    ["Scheduled", "/p/demo/scheduled"],
    ["Newsletters", "/p/demo/newsletters"],
  ])
    expect(screen.getByRole("link", { name })).toHaveAttribute("href", href);
});

it("builds an issue from blocks, previews it and saves a draft", async () => {
  vi.spyOn(admin, "get").mockResolvedValue({ items: [] } as never);
  const send = vi.spyOn(admin, "send").mockImplementation(async (path: string) => {
    if (path.endsWith("/preview")) return { html: "<h2>Hi</h2>", text: "HI" } as never;
    return { id: "n1" } as never;
  });
  wrap(<NewsletterEditor />, "/p/demo/newsletters/new", "/p/demo/newsletters/:id");
  expect(screen.getByText(/cannot be removed/)).toBeInTheDocument();
  // Nothing can be sent before the draft is saved.
  expect(screen.getByRole("button", { name: "Send me a test" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Send now" })).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "November" } });
  fireEvent.change(screen.getByLabelText("Heading 1"), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /Button/ }));
  fireEvent.change(screen.getByLabelText("Web address 3"), {
    target: { value: "https://example.com" },
  });
  expect(await screen.findByText("HI")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Phone width" }));
  expect(screen.getByTitle("Preview")).toHaveStyle({ maxWidth: "375px" });
  fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith(
      expect.stringMatching(/\/newsletters$/),
      "POST",
      expect.objectContaining({ name: "November" }),
    ),
  );
});
