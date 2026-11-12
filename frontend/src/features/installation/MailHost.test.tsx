import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "@/features/workspace/context";
import { MailHost } from "./MailHost";

afterEach(() => vi.restoreAllMocks());

it("shows receipts, domains missing a key, evidence flow and the rotation's DNS state", async () => {
  vi.spyOn(admin, "get").mockResolvedValue({
    receipts: [
      {
        id: "r1",
        action: "apply",
        result_code: "ok",
        applied_version: "3.8.6",
        approved_by: "cli",
        rollback_state: "none",
        verified_at: "2026-11-12T08:00:00Z",
        created_at: "2026-11-12T08:00:00Z",
        plan_digest: "abcdef012345",
      },
    ],
    evidence: {
      smtp: { last_at: "2026-11-12T09:00:00Z", events_24h: 4 },
      mta_log: { last_at: null, events_24h: 0 },
      dsn: { last_at: null, events_24h: 0 },
      submitted_24h: 4,
    },
    domains_missing_key: ["second.example.org"],
    suggested_commands: ["njiwa-mail installer plan -hostname m.example.com"],
    dkim_rotation: {
      state: "prepared",
      from: "njiwa",
      to: "njiwa2",
      ready_to_activate: false,
      dns: [
        {
          domain: "a.example",
          host: "njiwa2._domainkey.a.example",
          published: false,
          hint: "No DKIM record was found",
        },
      ],
    },
  } as never);
  render(
    <MemoryRouter>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <WorkspaceProvider project="demo">
          <MailHost />
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("second.example.org")).toBeInTheDocument();
  expect(screen.getByText("njiwa-mail installer plan -hostname m.example.com")).toBeInTheDocument();
  expect(screen.getByText("abcdef012345")).toBeInTheDocument();
  expect(screen.getByText(/no log evidence arrived/)).toBeInTheDocument();
  expect(screen.getByText("not published")).toBeInTheDocument();
  expect(screen.getByText(/Publish the new records first/)).toBeInTheDocument();
});
