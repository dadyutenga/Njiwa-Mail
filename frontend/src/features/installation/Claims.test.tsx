import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "@/features/workspace/context";
import { DomainClaims } from "./Claims";

afterEach(() => vi.restoreAllMocks());

it("lists claims, flags domains needing review and transfers with confirmation", async () => {
  vi.spyOn(admin, "get").mockResolvedValue({
    items: [
      {
        name: "dup.example.com",
        state: "review_required",
        project: null,
        projects_with_domain: ["one", "two"],
        claimed_at: "2026-11-01T00:00:00Z",
        updated_at: "2026-11-01T00:00:00Z",
      },
      {
        name: "notify.example.com",
        state: "active",
        project: "alpha",
        projects_with_domain: ["alpha"],
        claimed_at: "2026-11-02T00:00:00Z",
        updated_at: "2026-11-02T00:00:00Z",
      },
    ],
  } as never);
  const send = vi.spyOn(admin, "send").mockResolvedValue({} as never);
  render(
    <MemoryRouter>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <WorkspaceProvider project="demo">
          <DomainClaims />
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "1 domain(s) were verified by more than one project",
  );
  const owned = document.querySelector('[data-claim="notify.example.com"]') as HTMLElement;
  expect(within(owned).getByText("Owned by alpha")).toBeInTheDocument();
  expect(within(owned).getByText("No other project has added it")).toBeInTheDocument();
  const dup = document.querySelector('[data-claim="dup.example.com"]') as HTMLElement;
  fireEvent.change(within(dup).getByLabelText("Transfer to project"), { target: { value: "two" } });
  fireEvent.click(within(dup).getByRole("button", { name: "Assign" }));
  expect(await screen.findByText(/ownership proof for it is reset/)).toBeInTheDocument();
  fireEvent.click(screen.getAllByRole("button", { name: "Assign" }).at(-1)!);
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/domain-claims/transfer", "POST", {
      name: "dup.example.com",
      project: "two",
    }),
  );
});
