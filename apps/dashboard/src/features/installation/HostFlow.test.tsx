import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "@/features/workspace/context";
import { HostFlow } from "./HostFlow";

afterEach(() => vi.restoreAllMocks());

function open(steps: [string, string][]) {
  vi.spyOn(admin, "get").mockResolvedValue({
    profile_id: "p1",
    steps: steps.map(([id, state]) => ({ id, state, detail: `about ${id}` })),
  } as never);
  render(
    <MemoryRouter>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <WorkspaceProvider project="demo">
          <HostFlow />
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

it("offers the action of the next step only", async () => {
  const send = vi.spyOn(admin, "send").mockResolvedValue({} as never);
  open([
    ["scan", "done"],
    ["test", "current"],
    ["activate", "todo"],
  ]);
  fireEvent.click(await screen.findByRole("button", { name: "Run connection test" }));
  expect(screen.queryByRole("button", { name: "Activate" })).not.toBeInTheDocument();
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/installation/transport-profiles/p1/test", "POST"),
  );
});

it("sends a host with a mail server to connect it instead", async () => {
  open([
    ["scan", "done"],
    ["choose", "blocked"],
    ["install", "blocked"],
  ]);
  expect(
    await screen.findByRole("link", { name: "Connect the existing mail server" }),
  ).toHaveAttribute("href", "/p/demo/mail-server");
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});
