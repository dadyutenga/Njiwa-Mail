import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "@/features/workspace/context";
import { RelayStatus } from "./Tools";

afterEach(() => {
  vi.restoreAllMocks();
});

function open(status: Record<string, unknown>) {
  vi.spyOn(admin, "get").mockResolvedValue(status as never);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <WorkspaceProvider project="demo">
          <RelayStatus />
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

const base = { host: "smtp.example.org", host_source: "setting", port: "2587", tls: "starttls" };

it("shows the real settings, the error and the next step", async () => {
  open({ ...base, state: "error", detail: "cannot read the TLS certificate: permission denied" });
  expect(await screen.findByText(/permission denied/)).toBeInTheDocument();
  expect(screen.getByText("Error")).toBeInTheDocument();
  expect(screen.getByText(/retries by itself/)).toBeInTheDocument();
  expect(screen.getByText(/Host: smtp\.example\.org/)).toHaveTextContent("Port: 2587");
  expect(screen.queryByText(/mail\.example\.com/)).not.toBeInTheDocument();
});

it("says when the relay is not enabled", async () => {
  open({ ...base, state: "not_enabled" });
  expect(await screen.findByText("Not enabled")).toBeInTheDocument();
  expect(screen.getByText(/COMPOSE_PROFILES=relay/)).toBeInTheDocument();
});

it("sends a test and shows each step and what it did not prove", async () => {
  const send = vi.spyOn(admin, "send").mockResolvedValue({
    ok: false,
    address: "relay:2587",
    tls_name: "smtp.example.org",
    message_id: "",
    steps: [
      { key: "connect", title: "Connected to the relay", ok: true, detail: "connected" },
      { key: "tls", title: "TLS certificate valid", ok: false, detail: "unknown authority" },
      {
        key: "auth",
        title: "Signed in with a temporary API key",
        ok: false,
        skipped: true,
        detail: "",
      },
    ],
  } as never);
  open({ ...base, state: "running" });
  fireEvent.click(await screen.findByRole("button", { name: "Send a test through the relay" }));
  await waitFor(() => expect(send).toHaveBeenCalledWith("/w/demo/sandbox/relay-test", "POST"));
  const result = await screen.findByTestId("relay-test");
  expect(result).toHaveTextContent("The relay test failed");
  expect(within(result).getByText("unknown authority")).toBeInTheDocument();
  expect(result).toHaveTextContent("does not prove that your application can reach");
});
