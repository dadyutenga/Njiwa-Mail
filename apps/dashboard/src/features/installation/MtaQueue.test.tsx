import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "@/features/workspace/context";
import { MtaQueue } from "./MtaQueue";

afterEach(() => vi.restoreAllMocks());

function mount() {
  render(
    <MemoryRouter>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <WorkspaceProvider project="demo">
          <MtaQueue />
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

it("explains that the agent is not set up", async () => {
  vi.spyOn(admin, "get").mockResolvedValue({ configured: false } as never);
  mount();
  expect(await screen.findByText("The queue agent is not set up")).toBeInTheDocument();
});

it("shows masked recipients and only deletes after the queue id is typed", async () => {
  vi.spyOn(admin, "get").mockImplementation(async (path: string) => {
    if (path === "/mta/status")
      return {
        configured: true,
        reachable: true,
        status: { queue: { deferred: 1 }, total: 1, postfix_active: true, signer_active: false },
      } as never;
    return {
      items: [
        {
          queue_id: "4C1D2E3F4A",
          queue: "deferred",
          arrived_at: "2026-11-12T08:00:00Z",
          recipients: [{ address: "u***@example.org", delay_reason: "Connection timed out" }],
          message: { message_id: "m1", template: "otp" },
        },
      ],
    } as never;
  });
  const send = vi.spyOn(admin, "send").mockResolvedValue({ ok: true } as never);
  mount();
  expect(
    await screen.findByText(/u\*\*\*@example\.org — Connection timed out/),
  ).toBeInTheDocument();
  expect(screen.getByText("stopped")).toBeInTheDocument(); // the DKIM signer
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  const confirm = await screen.findByRole("button", { name: "Delete from queue" });
  expect(confirm).toBeDisabled();
  fireEvent.change(screen.getByRole("textbox"), { target: { value: "WRONG" } });
  expect(confirm).toBeDisabled();
  fireEvent.change(screen.getByRole("textbox"), { target: { value: "4c1d2e3f4a" } });
  expect(confirm).toBeEnabled();
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/mta/queue/4C1D2E3F4A/delete", "POST", {
      confirm: "4c1d2e3f4a",
    }),
  );
});
