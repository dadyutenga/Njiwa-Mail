import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "@/features/workspace/context";
import { ArchivedProjects, KeyCounts, ProjectActions } from "./ProjectLifecycle";

afterEach(() => {
  vi.restoreAllMocks();
});

function wrap(ui: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <WorkspaceProvider project="demo">{ui}</WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

it("shows active and revoked keys per environment", () => {
  wrap(
    <KeyCounts keys={{ live_active: 2, live_revoked: 1, sandbox_active: 3, sandbox_revoked: 0 }} />,
  );
  expect(screen.getByText(/Live: 2 active, 1 revoked/)).toBeInTheDocument();
  expect(screen.getByText(/Sandbox: 3 active, 0 revoked/)).toBeInTheDocument();
});

it("archives only after the project name is typed, and lists what is affected", async () => {
  vi.spyOn(admin, "get").mockResolvedValue({ api_keys: 4, queued_live: 0 } as never);
  const send = vi.spyOn(admin, "send").mockResolvedValue(undefined as never);
  wrap(<ProjectActions name="shop" />);
  fireEvent.click(screen.getByRole("button", { name: "Archive" }));
  expect(await screen.findByTestId("removal-counts")).toHaveTextContent("API keys4");
  const confirm = screen.getByRole("button", { name: "Archive project" });
  expect(confirm).toBeDisabled();
  fireEvent.change(screen.getByLabelText(/Type the project name/), { target: { value: "shop" } });
  expect(confirm).toBeEnabled();
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/projects/shop/archive", "POST", { confirm: "shop" }),
  );
});

it("blocks removal while Live mail is waiting, and says how much", async () => {
  vi.spyOn(admin, "get").mockImplementation(async (path: string) =>
    path.includes("removal")
      ? ({ queued_live: 7 } as never)
      : ({
          items: [{ id: "1", name: "old", role: "owner", purge_after: "2026-12-28T00:00:00Z" }],
        } as never),
  );
  wrap(<ArchivedProjects />);
  fireEvent.click(await screen.findByRole("button", { name: "Delete permanently" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("7");
  fireEvent.change(screen.getByLabelText(/Type the project name/), { target: { value: "old" } });
  expect(screen.getAllByRole("button", { name: "Delete permanently" }).at(-1)).toBeDisabled();
});
